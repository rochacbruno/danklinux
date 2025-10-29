package dwl

import (
	"fmt"
	"time"

	wlclient "github.com/yaslama/go-wayland/wayland/client"

	"github.com/AvengeMedia/danklinux/internal/errdefs"
	"github.com/AvengeMedia/danklinux/internal/log"
	"github.com/AvengeMedia/danklinux/internal/proto/dwl_ipc"
)

func NewManager() (*Manager, error) {
	display, err := wlclient.Connect("")
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errdefs.ErrNoWaylandDisplay, err)
	}

	m := &Manager{
		display:     display,
		outputs:     make(map[uint32]*outputState),
		cmdq:        make(chan cmd, 128),
		stopChan:    make(chan struct{}),
		subscribers: make(map[string]chan State),
		dirty:       make(chan struct{}, 1),
		layouts:     make([]string, 0),
	}

	if err := m.setupRegistry(); err != nil {
		display.Context().Close()
		return nil, err
	}

	m.updateState()

	m.notifierWg.Add(1)
	go m.notifier()

	m.wg.Add(1)
	go m.waylandActor()

	m.wg.Add(1)
	go m.eventDispatcher()

	return m, nil
}

func (m *Manager) post(fn func()) {
	select {
	case m.cmdq <- cmd{fn: fn}:
	default:
		log.Warn("DWL actor command queue full, dropping command")
	}
}

func (m *Manager) waylandActor() {
	defer m.wg.Done()

	for {
		select {
		case <-m.stopChan:
			return
		case c := <-m.cmdq:
			c.fn()
		}
	}
}

func (m *Manager) eventDispatcher() {
	defer m.wg.Done()
	ctx := m.display.Context()

	for {
		select {
		case <-m.stopChan:
			return
		default:
			if err := ctx.Dispatch(); err != nil {
				log.Errorf("DWL Wayland connection error: %v", err)
				return
			}
		}
	}
}

func (m *Manager) setupRegistry() error {
	log.Info("DWL: starting registry setup")
	ctx := m.display.Context()

	registry, err := m.display.GetRegistry()
	if err != nil {
		return fmt.Errorf("failed to get registry: %w", err)
	}
	m.registry = registry

	outputs := make([]*wlclient.Output, 0)
	outputRegNames := make(map[uint32]uint32)
	var dwlMgr *dwl_ipc.ZdwlIpcManagerV2

	registry.SetGlobalHandler(func(e wlclient.RegistryGlobalEvent) {
		switch e.Interface {
		case dwl_ipc.ZdwlIpcManagerV2InterfaceName:
			log.Infof("DWL: found %s", dwl_ipc.ZdwlIpcManagerV2InterfaceName)
			manager := dwl_ipc.NewZdwlIpcManagerV2(ctx)
			version := e.Version
			if version > 1 {
				version = 1
			}
			if err := registry.Bind(e.Name, e.Interface, version, manager); err == nil {
				dwlMgr = manager
				log.Info("DWL: manager bound successfully")
			} else {
				log.Errorf("DWL: failed to bind manager: %v", err)
			}
		case "wl_output":
			log.Debugf("DWL: found wl_output (name=%d)", e.Name)
			output := wlclient.NewOutput(ctx)
			version := e.Version
			if version > 4 {
				version = 4
			}
			if err := registry.Bind(e.Name, e.Interface, version, output); err == nil {
				outputID := output.ID()
				log.Infof("DWL: Bound wl_output id=%d registry_name=%d", outputID, e.Name)
				outputs = append(outputs, output)
				outputRegNames[outputID] = e.Name
			} else {
				log.Errorf("DWL: Failed to bind wl_output: %v", err)
			}
		}
	})

	registry.SetGlobalRemoveHandler(func(e wlclient.RegistryGlobalRemoveEvent) {
		m.post(func() {
			m.outputsMutex.Lock()
			defer m.outputsMutex.Unlock()

			for id, out := range m.outputs {
				if out.registryName == e.Name {
					log.Infof("DWL: Output %d removed", id)
					delete(m.outputs, id)
					m.updateState()
					return
				}
			}
		})
	})

	if err := m.display.Roundtrip(); err != nil {
		return fmt.Errorf("first roundtrip failed: %w", err)
	}
	if err := m.display.Roundtrip(); err != nil {
		return fmt.Errorf("second roundtrip failed: %w", err)
	}

	if dwlMgr == nil {
		log.Error("DWL: manager not found in registry")
		return fmt.Errorf("dwl_ipc_manager_v2 not available")
	}

	dwlMgr.SetTagsHandler(func(e dwl_ipc.ZdwlIpcManagerV2TagsEvent) {
		log.Infof("DWL: Tags count: %d", e.Amount)
		m.tagCount = e.Amount
		m.updateState()
	})

	dwlMgr.SetLayoutHandler(func(e dwl_ipc.ZdwlIpcManagerV2LayoutEvent) {
		log.Infof("DWL: Layout: %s", e.Name)
		m.layouts = append(m.layouts, e.Name)
		m.updateState()
	})

	m.manager = dwlMgr

	for _, output := range outputs {
		if err := m.setupOutput(dwlMgr, output, outputRegNames[output.ID()]); err != nil {
			log.Warnf("DWL: Failed to setup output %d: %v", output.ID(), err)
		}
	}

	if err := m.display.Roundtrip(); err != nil {
		return fmt.Errorf("final roundtrip failed: %w", err)
	}

	log.Info("DWL: registry setup complete")
	return nil
}

func (m *Manager) setupOutput(manager *dwl_ipc.ZdwlIpcManagerV2, output *wlclient.Output, regName uint32) error {
	ipcOutput, err := manager.GetOutput(output)
	if err != nil {
		return fmt.Errorf("failed to get dwl output: %w", err)
	}

	outState := &outputState{
		id:           output.ID(),
		registryName: regName,
		output:       output,
		ipcOutput:    ipcOutput,
		tags:         make([]TagState, 0),
	}

	ipcOutput.SetToggleVisibilityHandler(func(e dwl_ipc.ZdwlIpcOutputV2ToggleVisibilityEvent) {
		log.Debug("DWL: Toggle visibility event")
	})

	ipcOutput.SetActiveHandler(func(e dwl_ipc.ZdwlIpcOutputV2ActiveEvent) {
		log.Debugf("DWL: Output %d active: %d", outState.id, e.Active)
		outState.active = e.Active
		m.updateState()
	})

	ipcOutput.SetTagHandler(func(e dwl_ipc.ZdwlIpcOutputV2TagEvent) {
		log.Debugf("DWL: Output %d tag %d: state=%d clients=%d focused=%d",
			outState.id, e.Tag, e.State, e.Clients, e.Focused)

		for i, tag := range outState.tags {
			if tag.Tag == e.Tag {
				outState.tags[i] = TagState{
					Tag:     e.Tag,
					State:   e.State,
					Clients: e.Clients,
					Focused: e.Focused,
				}
				m.updateState()
				return
			}
		}

		outState.tags = append(outState.tags, TagState{
			Tag:     e.Tag,
			State:   e.State,
			Clients: e.Clients,
			Focused: e.Focused,
		})
		m.updateState()
	})

	ipcOutput.SetLayoutHandler(func(e dwl_ipc.ZdwlIpcOutputV2LayoutEvent) {
		log.Debugf("DWL: Output %d layout: %d", outState.id, e.Layout)
		outState.layout = e.Layout
		m.updateState()
	})

	ipcOutput.SetTitleHandler(func(e dwl_ipc.ZdwlIpcOutputV2TitleEvent) {
		log.Debugf("DWL: Output %d title: %s", outState.id, e.Title)
		outState.title = e.Title
		m.updateState()
	})

	ipcOutput.SetAppidHandler(func(e dwl_ipc.ZdwlIpcOutputV2AppidEvent) {
		log.Debugf("DWL: Output %d appid: %s", outState.id, e.Appid)
		outState.appID = e.Appid
		m.updateState()
	})

	ipcOutput.SetLayoutSymbolHandler(func(e dwl_ipc.ZdwlIpcOutputV2LayoutSymbolEvent) {
		log.Debugf("DWL: Output %d layout symbol: %s", outState.id, e.Layout)
		outState.layoutSymbol = e.Layout
		m.updateState()
	})

	ipcOutput.SetFrameHandler(func(e dwl_ipc.ZdwlIpcOutputV2FrameEvent) {
		log.Debugf("DWL: Output %d frame", outState.id)
		m.updateState()
	})

	m.outputsMutex.Lock()
	m.outputs[output.ID()] = outState
	m.outputsMutex.Unlock()

	return nil
}

func (m *Manager) updateState() {
	m.outputsMutex.RLock()
	outputs := make(map[string]*OutputState)
	activeOutput := ""

	for _, out := range m.outputs {
		name := out.name
		if name == "" {
			name = fmt.Sprintf("output-%d", out.id)
		}

		outputs[name] = &OutputState{
			Name:         name,
			Active:       out.active,
			Tags:         out.tags,
			Layout:       out.layout,
			LayoutSymbol: out.layoutSymbol,
			Title:        out.title,
			AppID:        out.appID,
		}

		if out.active != 0 {
			activeOutput = name
		}
	}
	m.outputsMutex.RUnlock()

	newState := State{
		Outputs:      outputs,
		TagCount:     m.tagCount,
		Layouts:      m.layouts,
		ActiveOutput: activeOutput,
	}

	m.stateMutex.Lock()
	m.state = &newState
	m.stateMutex.Unlock()

	m.notifySubscribers()
}

func (m *Manager) notifier() {
	defer m.notifierWg.Done()
	const minGap = 100 * time.Millisecond
	var timer *time.Timer
	var pending bool

	for {
		select {
		case <-m.stopChan:
			return
		case <-m.dirty:
			if pending {
				continue
			}
			pending = true
			if timer != nil {
				timer.Stop()
			}
			timer = time.AfterFunc(minGap, func() {
				m.subMutex.RLock()
				if len(m.subscribers) == 0 {
					m.subMutex.RUnlock()
					pending = false
					return
				}

				currentState := m.GetState()

				if m.lastNotified != nil && !stateChanged(m.lastNotified, &currentState) {
					m.subMutex.RUnlock()
					pending = false
					return
				}

				for _, ch := range m.subscribers {
					select {
					case ch <- currentState:
					default:
					}
				}
				m.subMutex.RUnlock()

				stateCopy := currentState
				m.lastNotified = &stateCopy
				pending = false
			})
		}
	}
}

func (m *Manager) SetTags(outputName string, tagmask uint32, toggleTagset uint32) error {
	m.outputsMutex.RLock()
	defer m.outputsMutex.RUnlock()

	for _, out := range m.outputs {
		name := out.name
		if name == "" {
			name = fmt.Sprintf("output-%d", out.id)
		}
		if name == outputName {
			ipcOut := out.ipcOutput.(*dwl_ipc.ZdwlIpcOutputV2)
			return ipcOut.SetTags(tagmask, toggleTagset)
		}
	}

	return fmt.Errorf("output not found: %s", outputName)
}

func (m *Manager) SetClientTags(outputName string, andTags uint32, xorTags uint32) error {
	m.outputsMutex.RLock()
	defer m.outputsMutex.RUnlock()

	for _, out := range m.outputs {
		name := out.name
		if name == "" {
			name = fmt.Sprintf("output-%d", out.id)
		}
		if name == outputName {
			ipcOut := out.ipcOutput.(*dwl_ipc.ZdwlIpcOutputV2)
			return ipcOut.SetClientTags(andTags, xorTags)
		}
	}

	return fmt.Errorf("output not found: %s", outputName)
}

func (m *Manager) SetLayout(outputName string, index uint32) error {
	m.outputsMutex.RLock()
	defer m.outputsMutex.RUnlock()

	for _, out := range m.outputs {
		name := out.name
		if name == "" {
			name = fmt.Sprintf("output-%d", out.id)
		}
		if name == outputName {
			ipcOut := out.ipcOutput.(*dwl_ipc.ZdwlIpcOutputV2)
			return ipcOut.SetLayout(index)
		}
	}

	return fmt.Errorf("output not found: %s", outputName)
}

func (m *Manager) Close() {
	close(m.stopChan)
	m.wg.Wait()
	m.notifierWg.Wait()

	m.subMutex.Lock()
	for _, ch := range m.subscribers {
		close(ch)
	}
	m.subscribers = make(map[string]chan State)
	m.subMutex.Unlock()

	m.outputsMutex.Lock()
	for _, out := range m.outputs {
		if ipcOut, ok := out.ipcOutput.(*dwl_ipc.ZdwlIpcOutputV2); ok {
			ipcOut.Release()
		}
	}
	m.outputs = make(map[uint32]*outputState)
	m.outputsMutex.Unlock()

	if mgr, ok := m.manager.(*dwl_ipc.ZdwlIpcManagerV2); ok {
		mgr.Release()
	}

	if m.display != nil {
		m.display.Context().Close()
	}
}
