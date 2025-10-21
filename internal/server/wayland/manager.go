package wayland

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"syscall"
	"time"

	wlclient "github.com/yaslama/go-wayland/wayland/client"
	"golang.org/x/sys/unix"

	"github.com/AvengeMedia/danklinux/internal/errdefs"
	"github.com/AvengeMedia/danklinux/internal/log"
	"github.com/AvengeMedia/danklinux/internal/proto/wlr_gamma_control"
)

func NewManager(config Config) (*Manager, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	display, err := wlclient.Connect("")
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errdefs.ErrNoWaylandDisplay, err)
	}

	m := &Manager{
		config:        config,
		display:       display,
		outputs:       make(map[uint32]*outputState),
		cmdq:          make(chan cmd, 128),
		stopChan:      make(chan struct{}),
		updateTrigger: make(chan struct{}, 1),
		subscribers:   make(map[string]chan State),
		dirty:         make(chan struct{}, 1),
	}

	if err := m.setupRegistry(); err != nil {
		display.Context().Close()
		return nil, err
	}

	// Initialize currentTemp and targetTemp before starting any goroutines
	now := time.Now()
	initial := m.calculateTemperature(now)
	m.transitionMutex.Lock()
	m.currentTemp = initial
	m.targetTemp = initial
	m.transitionMutex.Unlock()

	m.alive = true
	m.updateState()

	m.notifierWg.Add(1)
	go m.notifier()

	m.wg.Add(1)
	go m.updateLoop()

	m.wg.Add(1)
	go m.waylandActor()

	m.wg.Add(1)
	go m.eventDispatcher()

	if config.Enabled {
		m.post(func() {
			log.Info("Gamma control enabled at startup, initializing controls")
			gammaMgr := m.gammaControl.(*wlr_gamma_control.ZwlrGammaControlManagerV1)
			if err := m.setupOutputControls(m.availableOutputs, gammaMgr, true); err != nil {
				log.Errorf("Failed to initialize gamma controls: %v", err)
			} else {
				m.controlsInitialized = true
			}
		})
	}

	return m, nil
}

func (m *Manager) post(fn func()) {
	select {
	case m.cmdq <- cmd{fn: fn}:
	default:
		log.Warn("Actor command queue full, dropping command")
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
				log.Errorf("Wayland connection error: %v", err)
				m.handleDisconnect(err)
				return
			}
		}
	}
}

func (m *Manager) allOutputsReady() bool {
	m.outputsMutex.RLock()
	defer m.outputsMutex.RUnlock()
	if len(m.outputs) == 0 {
		return false
	}
	for _, o := range m.outputs {
		if o.rampSize == 0 || o.failed {
			return false
		}
	}
	return true
}

func (m *Manager) handleDisconnect(err error) {
	log.Warnf("Wayland disconnected: %v, attempting reconnect...", err)
	m.alive = false

	m.outputs = make(map[uint32]*outputState)
	m.controlsInitialized = false

	backoff := time.Second
	for {
		select {
		case <-m.stopChan:
			return
		default:
		}

		display, derr := wlclient.Connect("")
		if derr == nil {
			m.display = display
			break
		}
		log.Warnf("Reconnect failed: %v, retrying in %v", derr, backoff)
		time.Sleep(backoff)
		if backoff < 8*time.Second {
			backoff *= 2
		}
	}

	if err := m.setupRegistry(); err != nil {
		log.Errorf("Failed to setup registry after reconnect: %v", err)
		// Restart only the dispatcher, not the actor
		m.wg.Add(1)
		go m.eventDispatcher()
		return
	}

	m.configMutex.RLock()
	enabled := m.config.Enabled
	m.configMutex.RUnlock()

	if enabled {
		gammaMgr := m.gammaControl.(*wlr_gamma_control.ZwlrGammaControlManagerV1)
		if err := m.setupOutputControls(m.availableOutputs, gammaMgr, true); err == nil {
			m.controlsInitialized = true
			m.transitionMutex.RLock()
			temp := m.targetTemp
			m.transitionMutex.RUnlock()
			m.applyNowOnActor(temp)
		}
	}

	m.alive = true
	log.Info("Wayland reconnected successfully")
	// Restart only the dispatcher, actor is still running
	m.wg.Add(1)
	go m.eventDispatcher()
}

func (m *Manager) setupRegistry() error {
	log.Info("setupRegistry: starting registry setup")
	ctx := m.display.Context()

	registry, err := m.display.GetRegistry()
	if err != nil {
		return fmt.Errorf("failed to get registry: %w", err)
	}
	m.registry = registry
	log.Debug("setupRegistry: registry obtained")

	outputs := make([]*wlclient.Output, 0)
	outputRegNames := make(map[uint32]uint32)
	var gammaMgr *wlr_gamma_control.ZwlrGammaControlManagerV1

	registry.SetGlobalHandler(func(e wlclient.RegistryGlobalEvent) {
		switch e.Interface {
		case wlr_gamma_control.ZwlrGammaControlManagerV1InterfaceName:
			log.Infof("setupRegistry: found %s", wlr_gamma_control.ZwlrGammaControlManagerV1InterfaceName)
			manager := wlr_gamma_control.NewZwlrGammaControlManagerV1(ctx)
			version := e.Version
			if version > 1 {
				version = 1
			}
			if err := registry.Bind(e.Name, e.Interface, version, manager); err == nil {
				gammaMgr = manager
				log.Info("setupRegistry: gamma control manager bound successfully")
			} else {
				log.Errorf("setupRegistry: failed to bind gamma control: %v", err)
			}
		case "wl_output":
			log.Debugf("Global event: found wl_output (name=%d)", e.Name)
			output := wlclient.NewOutput(ctx)
			version := e.Version
			if version > 4 {
				version = 4
			}
			if err := registry.Bind(e.Name, e.Interface, version, output); err == nil {
				outputID := output.ID()
				log.Infof("Bound wl_output id=%d registry_name=%d", outputID, e.Name)

				if gammaMgr != nil {
					outputs = append(outputs, output)
					outputRegNames[outputID] = e.Name
				}

				m.outputsMutex.Lock()
				if m.outputRegNames != nil {
					m.outputRegNames[outputID] = e.Name
				}
				m.outputsMutex.Unlock()

				m.configMutex.RLock()
				enabled := m.config.Enabled
				m.configMutex.RUnlock()

				if enabled && m.controlsInitialized {
					m.post(func() {
						log.Infof("New output %d added, creating gamma control", outputID)
						if err := m.addOutputControl(output); err != nil {
							log.Errorf("Failed to add gamma control for new output %d: %v", outputID, err)
						}
					})
				} else if enabled && !m.controlsInitialized {
					log.Infof("Output %d added but controls not initialized, will be handled on enable", outputID)
				}
			} else {
				log.Errorf("Failed to bind wl_output: %v", err)
			}
		}
	})

	registry.SetGlobalRemoveHandler(func(e wlclient.RegistryGlobalRemoveEvent) {
		m.post(func() {
			m.outputsMutex.Lock()
			defer m.outputsMutex.Unlock()

			for id, out := range m.outputs {
				if out.registryName == e.Name {
					log.Infof("Output %d (registry name %d) removed, destroying gamma control", id, e.Name)
					if out.gammaControl != nil {
						control := out.gammaControl.(*wlr_gamma_control.ZwlrGammaControlV1)
						control.Destroy()
					}
					delete(m.outputs, id)

					if len(m.outputs) == 0 {
						m.controlsInitialized = false
						log.Info("All outputs removed, controls no longer initialized")
					}
					return
				}
			}
		})
	})

	log.Debug("setupRegistry: performing roundtrips")
	if err := m.display.Roundtrip(); err != nil {
		return fmt.Errorf("first roundtrip failed: %w", err)
	}
	if err := m.display.Roundtrip(); err != nil {
		return fmt.Errorf("second roundtrip failed: %w", err)
	}

	log.Infof("setupRegistry: discovered gamma_manager=%v, outputs=%d", gammaMgr != nil, len(outputs))

	if gammaMgr == nil {
		log.Error("setupRegistry: gamma control manager not found in registry")
		return errdefs.ErrNoGammaControl
	}

	if len(outputs) == 0 {
		log.Error("setupRegistry: no wl_output objects found")
		return fmt.Errorf("no outputs available")
	}

	m.gammaControl = gammaMgr
	m.availableOutputs = outputs
	m.outputRegNames = outputRegNames

	log.Info("setupRegistry: completed successfully (gamma controls will be initialized when enabled)")
	return nil
}

func (m *Manager) setupOutputControls(outputs []*wlclient.Output, manager *wlr_gamma_control.ZwlrGammaControlManagerV1, doRoundtrip bool) error {
	log.Infof("setupOutputControls: creating gamma controls for %d outputs", len(outputs))

	for i, output := range outputs {
		log.Debugf("setupOutputControls: Loop iteration %d, getting gamma control for output %d", i, output.ID())
		control, err := manager.GetGammaControl(output)
		if err != nil {
			log.Warnf("Failed to get gamma control for output %d: %v", output.ID(), err)
			continue
		}
		log.Debugf("setupOutputControls: Successfully got control for output %d", output.ID())

		outState := &outputState{
			id:           output.ID(),
			registryName: m.outputRegNames[output.ID()],
			output:       output,
			gammaControl: control,
		}

		func(state *outputState) {
			control.SetGammaSizeHandler(func(e wlr_gamma_control.ZwlrGammaControlV1GammaSizeEvent) {
				state.rampSize = e.Size
				state.failed = false
				log.Infof("Output %d gamma_size=%d", state.id, e.Size)

				if m.allOutputsReady() {
					m.triggerUpdate()
				}
			})

			control.SetFailedHandler(func(e wlr_gamma_control.ZwlrGammaControlV1FailedEvent) {
				log.Errorf("Gamma control FAILED for output %d - marking for recreation", state.id)
				m.outputsMutex.Lock()
				if out, exists := m.outputs[state.id]; exists {
					out.failed = true
					out.rampSize = 0
				}
				m.outputsMutex.Unlock()

				// Schedule recreation with backoff
				time.AfterFunc(300*time.Millisecond, func() {
					m.post(func() {
						log.Debugf("Attempting to recreate gamma control for output %d", state.id)
						_ = m.recreateOutputControl(state)
					})
				})
			})
		}(outState)

		m.outputsMutex.Lock()
		m.outputs[output.ID()] = outState
		m.outputsMutex.Unlock()

		log.Debugf("setupOutputControls: Completed iteration %d for output %d", i, output.ID())
	}

	log.Debugf("setupOutputControls: Loop completed, processed %d outputs", len(outputs))

	if doRoundtrip {
		log.Debug("setupOutputControls: performing roundtrip to receive gamma_size events")
		if err := m.display.Roundtrip(); err != nil {
			log.Errorf("Roundtrip failed: %v", err)
			return fmt.Errorf("roundtrip after control creation failed: %w", err)
		}
		log.Debug("setupOutputControls: Roundtrip completed successfully")

		m.outputsMutex.RLock()
		readyCount := 0
		for _, out := range m.outputs {
			if out.rampSize > 0 {
				readyCount++
				log.Infof("Output %d: gamma control ready with size=%d", out.id, out.rampSize)
			} else {
				log.Warnf("Output %d: no gamma_size received yet (rampSize=0)", out.id)
			}
		}
		m.outputsMutex.RUnlock()

		log.Infof("setupOutputControls: completed, %d/%d outputs ready", readyCount, len(m.outputs))
	} else {
		log.Info("setupOutputControls: completed, gamma_size events will arrive via event loop")
	}

	return nil
}

func (m *Manager) addOutputControl(output *wlclient.Output) error {
	gammaMgr := m.gammaControl.(*wlr_gamma_control.ZwlrGammaControlManagerV1)

	control, err := gammaMgr.GetGammaControl(output)
	if err != nil {
		return fmt.Errorf("failed to get gamma control: %w", err)
	}

	outState := &outputState{
		id:           output.ID(),
		registryName: m.outputRegNames[output.ID()],
		output:       output,
		gammaControl: control,
	}

	control.SetGammaSizeHandler(func(e wlr_gamma_control.ZwlrGammaControlV1GammaSizeEvent) {
		outState.rampSize = e.Size
		outState.failed = false
		log.Infof("Output %d gamma_size=%d", outState.id, e.Size)

		if m.allOutputsReady() {
			m.triggerUpdate()
		}
	})

	control.SetFailedHandler(func(e wlr_gamma_control.ZwlrGammaControlV1FailedEvent) {
		log.Errorf("Gamma control FAILED for output %d - marking for recreation", outState.id)
		m.outputsMutex.Lock()
		if out, exists := m.outputs[outState.id]; exists {
			out.failed = true
			out.rampSize = 0
		}
		m.outputsMutex.Unlock()

		time.AfterFunc(300*time.Millisecond, func() {
			m.post(func() {
				log.Debugf("Attempting to recreate gamma control for output %d", outState.id)
				_ = m.recreateOutputControl(outState)
			})
		})
	})

	m.outputsMutex.Lock()
	m.outputs[output.ID()] = outState
	m.outputsMutex.Unlock()

	log.Infof("Added gamma control for output %d", output.ID())
	return nil
}

func (m *Manager) updateLoop() {
	defer m.wg.Done()

	targetTemp := m.calculateTemperature(time.Now())

	m.transitionMutex.Lock()
	m.currentTemp = targetTemp
	m.targetTemp = targetTemp
	m.transitionMutex.Unlock()

	m.applyGammaImmediate(targetTemp)

	var timer *time.Timer
	for {
		nextTransition := m.calculateNextTransition(time.Now())

		waitDuration := time.Until(nextTransition)
		if waitDuration < 0 {
			waitDuration = 1 * time.Second
		}

		if timer != nil {
			timer.Stop()
		}
		timer = time.NewTimer(waitDuration)

		select {
		case <-m.stopChan:
			if timer != nil {
				timer.Stop()
			}
			return
		case <-m.updateTrigger:
			debounceTimer := time.NewTimer(50 * time.Millisecond)
		drainLoop:
			for {
				select {
				case <-m.updateTrigger:
					debounceTimer.Reset(50 * time.Millisecond)
				case <-debounceTimer.C:
					break drainLoop
				case <-m.stopChan:
					debounceTimer.Stop()
					return
				}
			}

			m.configMutex.RLock()
			enabled := m.config.Enabled
			m.configMutex.RUnlock()
			if enabled {
				newTargetTemp := m.calculateTemperature(time.Now())
				m.startTransition(newTargetTemp)
			}
		case <-timer.C:
			// Drain any pending triggers to collapse bursts into one
			drain := true
			for drain {
				select {
				case <-m.updateTrigger:
					// keep draining
				default:
					drain = false
				}
			}
			// Recompute once, then kick a single transition (if enabled)
			m.configMutex.RLock()
			enabled := m.config.Enabled
			m.configMutex.RUnlock()
			if enabled {
				newTargetTemp := m.calculateTemperature(time.Now())
				m.startTransition(newTargetTemp)
			}
		}
	}
}

func (m *Manager) startTransition(targetTemp int) {
	if !m.controlsInitialized || !m.allOutputsReady() {
		m.transitionMutex.Lock()
		m.targetTemp = targetTemp
		m.transitionMutex.Unlock()
		log.Debugf("Controls not ready, deferring transition to %dK", targetTemp)
		return
	}

	m.transitionMutex.Lock()
	current := m.currentTemp
	m.targetTemp = targetTemp

	if current == targetTemp {
		m.transitionMutex.Unlock()
		log.Debugf("Skipping transition: already at %dK", targetTemp)
		return
	}

	m.transitionSerial++
	serial := m.transitionSerial
	m.transitionMutex.Unlock()

	go func(currentTemp, targetTemp int, mySerial int64) {
		const dur = 1 * time.Second
		const fps = 30
		steps := int(dur.Seconds() * fps)

		log.Debugf("Starting smooth transition: %dK -> %dK over %v", currentTemp, targetTemp, dur)

		for i := 0; i <= steps; i++ {
			m.transitionMutex.RLock()
			if m.transitionSerial != mySerial {
				m.transitionMutex.RUnlock()
				log.Debugf("Transition %dK -> %dK aborted (newer transition started)", currentTemp, targetTemp)
				return
			}
			m.transitionMutex.RUnlock()

			progress := float64(i) / float64(steps)
			temp := currentTemp + int(float64(targetTemp-currentTemp)*progress)

			m.post(func() { m.applyNowOnActor(temp) })

			if i < steps {
				time.Sleep(dur / time.Duration(steps))
			}
		}

		log.Debugf("Transition complete: now at %dK", targetTemp)

		m.configMutex.RLock()
		enabled := m.config.Enabled
		m.configMutex.RUnlock()

		const identityTemp = 6500
		if !enabled && targetTemp == identityTemp && m.controlsInitialized {
			m.post(func() {
				log.Info("Destroying gamma controls after transition to identity")
				m.outputsMutex.Lock()
				for id, out := range m.outputs {
					if out.gammaControl != nil {
						control := out.gammaControl.(*wlr_gamma_control.ZwlrGammaControlV1)
						control.Destroy()
						log.Debugf("Destroyed gamma control for output %d", id)
					}
				}
				m.outputs = make(map[uint32]*outputState)
				m.controlsInitialized = false
				m.outputsMutex.Unlock()

				m.transitionMutex.Lock()
				m.currentTemp = identityTemp
				m.targetTemp = identityTemp
				m.transitionMutex.Unlock()

				log.Info("All gamma controls destroyed")
			})
		}
	}(current, targetTemp, serial)
}

func (m *Manager) recreateOutputControl(out *outputState) error {
	gammaMgr, ok := m.gammaControl.(*wlr_gamma_control.ZwlrGammaControlManagerV1)
	if !ok || gammaMgr == nil {
		return fmt.Errorf("gamma control manager not available")
	}

	log.Debugf("Recreating gamma control for output %d", out.id)
	control, err := gammaMgr.GetGammaControl(out.output)
	if err != nil {
		return fmt.Errorf("get gamma control: %w", err)
	}

	state := out
	control.SetGammaSizeHandler(func(e wlr_gamma_control.ZwlrGammaControlV1GammaSizeEvent) {
		state.rampSize = e.Size
		state.failed = false
		log.Infof("Output %d gamma_size=%d (recreated)", state.id, e.Size)
	})

	control.SetFailedHandler(func(e wlr_gamma_control.ZwlrGammaControlV1FailedEvent) {
		log.Errorf("Gamma control FAILED again for output %d", state.id)
		m.outputsMutex.Lock()
		if outState, exists := m.outputs[state.id]; exists {
			outState.failed = true
			outState.rampSize = 0
		}
		m.outputsMutex.Unlock()

		// Schedule recreation with backoff
		time.AfterFunc(300*time.Millisecond, func() {
			m.post(func() {
				log.Debugf("Attempting to recreate gamma control for output %d (after re-fail)", state.id)
				_ = m.recreateOutputControl(state)
			})
		})
	})

	out.gammaControl = control
	out.failed = false

	if err := m.display.Roundtrip(); err != nil {
		return fmt.Errorf("roundtrip after recreation: %w", err)
	}

	return nil
}

func (m *Manager) applyGammaImmediate(temp int) {
	m.post(func() { m.applyNowOnActor(temp) })
}

func (m *Manager) applyNowOnActor(temp int) {
	m.configMutex.RLock()
	gamma := m.config.Gamma
	m.configMutex.RUnlock()

	if !m.controlsInitialized {
		return
	}

	// Lock while snapshotting outputs to prevent races with recreateOutputControl
	m.outputsMutex.RLock()
	var outs []*outputState
	for _, out := range m.outputs {
		outs = append(outs, out)
	}
	m.outputsMutex.RUnlock()

	if len(outs) == 0 {
		return
	}

	// Collect ready outputs & pack their buffers first (atomic apply)
	type job struct {
		out  *outputState
		data []byte
	}
	var jobs []job

	for _, out := range outs {
		if out.failed || out.rampSize == 0 {
			continue
		}

		ramp := GenerateGammaRamp(out.rampSize, temp, gamma)

		// Pack once into []byte
		buf := bytes.NewBuffer(make([]byte, 0, int(out.rampSize)*6))
		for _, v := range ramp.Red {
			_ = binary.Write(buf, binary.LittleEndian, v)
		}
		for _, v := range ramp.Green {
			_ = binary.Write(buf, binary.LittleEndian, v)
		}
		for _, v := range ramp.Blue {
			_ = binary.Write(buf, binary.LittleEndian, v)
		}

		jobs = append(jobs, job{out: out, data: buf.Bytes()})
	}

	// Now send to all ready outputs in this tick
	for _, j := range jobs {
		if err := m.setGammaBytesActor(j.out, j.data); err != nil {
			log.Warnf("Failed to set gamma for output %d: %v", j.out.id, err)
			outID := j.out.id
			m.outputsMutex.Lock()
			if out, exists := m.outputs[outID]; exists {
				out.failed = true
				out.rampSize = 0
			}
			m.outputsMutex.Unlock()

			time.AfterFunc(300*time.Millisecond, func() {
				m.post(func() {
					m.outputsMutex.RLock()
					out, exists := m.outputs[outID]
					m.outputsMutex.RUnlock()
					if exists && out.failed {
						log.Debugf("Attempting to recreate gamma control for failed output %d", outID)
						_ = m.recreateOutputControl(out)
					}
				})
			})
		}
	}

	m.transitionMutex.Lock()
	m.currentTemp = temp
	m.transitionMutex.Unlock()

	m.updateState()
}

func (m *Manager) setGammaBytesActor(out *outputState, data []byte) error {
	fd, err := MemfdCreate("gamma-ramp", 0)
	if err != nil {
		return fmt.Errorf("memfd_create: %w", err)
	}
	defer syscall.Close(fd)

	if err := syscall.Ftruncate(fd, int64(len(data))); err != nil {
		return fmt.Errorf("ftruncate: %w", err)
	}

	dupFd, err := syscall.Dup(fd)
	if err != nil {
		return fmt.Errorf("dup: %w", err)
	}
	f := os.NewFile(uintptr(dupFd), "gamma")
	defer f.Close()

	n, err := f.Write(data)
	if err != nil || n != len(data) {
		return fmt.Errorf("write gamma: %w (n=%d want=%d)", err, n, len(data))
	}

	if _, err := syscall.Seek(fd, 0, 0); err != nil {
		return fmt.Errorf("seek: %w", err)
	}

	ctrl := out.gammaControl.(*wlr_gamma_control.ZwlrGammaControlV1)
	if err := ctrl.SetGamma(fd); err != nil {
		return fmt.Errorf("SetGamma: %w", err)
	}

	return nil
}

func (m *Manager) updateState() {
	now := time.Now()

	m.configMutex.RLock()
	configCopy := m.config
	m.configMutex.RUnlock()

	var sunrise, sunset time.Time
	if configCopy.ManualSunrise != nil && configCopy.ManualSunset != nil {
		year, month, day := now.Date()
		loc := now.Location()
		sunrise = time.Date(year, month, day,
			configCopy.ManualSunrise.Hour(),
			configCopy.ManualSunrise.Minute(),
			configCopy.ManualSunrise.Second(), 0, loc)
		sunset = time.Date(year, month, day,
			configCopy.ManualSunset.Hour(),
			configCopy.ManualSunset.Minute(),
			configCopy.ManualSunset.Second(), 0, loc)
	} else if configCopy.UseIPLocation {
		lat, lon, err := m.getIPLocation()
		if err == nil {
			times := CalculateSunTimes(*lat, *lon, now)
			sunrise = times.Sunrise
			sunset = times.Sunset
		}
	} else if configCopy.Latitude != nil && configCopy.Longitude != nil {
		times := CalculateSunTimes(*configCopy.Latitude, *configCopy.Longitude, now)
		sunrise = times.Sunrise
		sunset = times.Sunset
	}

	m.transitionMutex.RLock()
	temp := m.currentTemp
	m.transitionMutex.RUnlock()

	nextTransition := m.calculateNextTransition(now)
	isDay := now.After(sunrise) && now.Before(sunset)

	newState := State{
		Config:         configCopy,
		CurrentTemp:    temp,
		NextTransition: nextTransition,
		SunriseTime:    sunrise,
		SunsetTime:     sunset,
		IsDay:          isDay,
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

func (m *Manager) triggerUpdate() {
	select {
	case m.updateTrigger <- struct{}{}:
	default:
	}
}

func (m *Manager) SetConfig(config Config) error {
	if err := config.Validate(); err != nil {
		return err
	}

	m.configMutex.Lock()
	m.config = config
	m.configMutex.Unlock()

	m.triggerUpdate()
	return nil
}

func (m *Manager) SetTemperature(low, high int) error {
	m.configMutex.Lock()
	m.config.LowTemp = low
	m.config.HighTemp = high
	err := m.config.Validate()
	m.configMutex.Unlock()

	if err != nil {
		return err
	}
	m.triggerUpdate()
	return nil
}

func (m *Manager) SetLocation(lat, lon float64) error {
	m.configMutex.Lock()
	m.config.Latitude = &lat
	m.config.Longitude = &lon
	m.config.UseIPLocation = false
	err := m.config.Validate()
	m.configMutex.Unlock()

	if err != nil {
		return err
	}
	m.triggerUpdate()
	return nil
}

func (m *Manager) SetUseIPLocation(use bool) {
	m.configMutex.Lock()
	m.config.UseIPLocation = use
	if use {
		m.config.Latitude = nil
		m.config.Longitude = nil
	}
	m.configMutex.Unlock()

	if use {
		m.locationMutex.Lock()
		m.cachedIPLat = nil
		m.cachedIPLon = nil
		m.locationMutex.Unlock()
	}

	m.triggerUpdate()
}

func (m *Manager) getIPLocation() (*float64, *float64, error) {
	m.locationMutex.RLock()
	if m.cachedIPLat != nil && m.cachedIPLon != nil {
		lat, lon := m.cachedIPLat, m.cachedIPLon
		m.locationMutex.RUnlock()
		return lat, lon, nil
	}
	m.locationMutex.RUnlock()

	lat, lon, err := FetchIPLocation()
	if err != nil {
		return nil, nil, err
	}

	m.locationMutex.Lock()
	m.cachedIPLat = lat
	m.cachedIPLon = lon
	m.locationMutex.Unlock()

	return lat, lon, nil
}

func (m *Manager) calculateTemperature(now time.Time) int {
	m.configMutex.RLock()
	config := m.config
	m.configMutex.RUnlock()

	if !config.Enabled {
		return config.HighTemp
	}

	var sunrise, sunset time.Time

	if config.ManualSunrise != nil && config.ManualSunset != nil {
		year, month, day := now.Date()
		loc := now.Location()

		sunrise = time.Date(year, month, day,
			config.ManualSunrise.Hour(),
			config.ManualSunrise.Minute(),
			config.ManualSunrise.Second(), 0, loc)
		sunset = time.Date(year, month, day,
			config.ManualSunset.Hour(),
			config.ManualSunset.Minute(),
			config.ManualSunset.Second(), 0, loc)

		if sunset.Before(sunrise) {
			sunset = sunset.Add(24 * time.Hour)
		}
	} else if config.UseIPLocation {
		lat, lon, err := m.getIPLocation()
		if err != nil {
			return config.HighTemp
		}
		times := CalculateSunTimes(*lat, *lon, now)
		sunrise = times.Sunrise
		sunset = times.Sunset
	} else if config.Latitude != nil && config.Longitude != nil {
		times := CalculateSunTimes(*config.Latitude, *config.Longitude, now)
		sunrise = times.Sunrise
		sunset = times.Sunset
	} else {
		return config.LowTemp
	}

	if now.Before(sunrise) || now.After(sunset) {
		return config.LowTemp
	}
	return config.HighTemp
}

func (m *Manager) calculateNextTransition(now time.Time) time.Time {
	m.configMutex.RLock()
	config := m.config
	m.configMutex.RUnlock()

	if !config.Enabled {
		return now.Add(24 * time.Hour)
	}

	var sunrise, sunset time.Time

	if config.ManualSunrise != nil && config.ManualSunset != nil {
		year, month, day := now.Date()
		loc := now.Location()

		sunrise = time.Date(year, month, day,
			config.ManualSunrise.Hour(),
			config.ManualSunrise.Minute(),
			config.ManualSunrise.Second(), 0, loc)
		sunset = time.Date(year, month, day,
			config.ManualSunset.Hour(),
			config.ManualSunset.Minute(),
			config.ManualSunset.Second(), 0, loc)

		if sunset.Before(sunrise) {
			sunset = sunset.Add(24 * time.Hour)
		}
	} else if config.UseIPLocation {
		lat, lon, err := m.getIPLocation()
		if err != nil {
			return now.Add(24 * time.Hour)
		}
		times := CalculateSunTimes(*lat, *lon, now)
		sunrise = times.Sunrise
		sunset = times.Sunset
	} else if config.Latitude != nil && config.Longitude != nil {
		times := CalculateSunTimes(*config.Latitude, *config.Longitude, now)
		sunrise = times.Sunrise
		sunset = times.Sunset
	} else {
		return now.Add(24 * time.Hour)
	}

	if now.Before(sunrise) {
		return sunrise
	}
	if now.Before(sunset) {
		return sunset
	}

	if config.ManualSunrise != nil && config.ManualSunset != nil {
		year, month, day := now.Add(24 * time.Hour).Date()
		loc := now.Location()
		nextSunrise := time.Date(year, month, day,
			config.ManualSunrise.Hour(),
			config.ManualSunrise.Minute(),
			config.ManualSunrise.Second(), 0, loc)
		return nextSunrise
	}

	if config.UseIPLocation {
		lat, lon, err := m.getIPLocation()
		if err != nil {
			return now.Add(24 * time.Hour)
		}
		nextDayTimes := CalculateSunTimes(*lat, *lon, now.Add(24*time.Hour))
		return nextDayTimes.Sunrise
	}

	if config.Latitude != nil && config.Longitude != nil {
		nextDayTimes := CalculateSunTimes(*config.Latitude, *config.Longitude, now.Add(24*time.Hour))
		return nextDayTimes.Sunrise
	}

	return now.Add(24 * time.Hour)
}

func (m *Manager) SetManualTimes(sunrise, sunset time.Time) error {
	m.configMutex.Lock()
	m.config.ManualSunrise = &sunrise
	m.config.ManualSunset = &sunset
	err := m.config.Validate()
	m.configMutex.Unlock()

	if err != nil {
		return err
	}
	m.triggerUpdate()
	return nil
}

func (m *Manager) ClearManualTimes() {
	m.configMutex.Lock()
	m.config.ManualSunrise = nil
	m.config.ManualSunset = nil
	m.configMutex.Unlock()
	m.triggerUpdate()
}

func (m *Manager) SetGamma(gamma float64) error {
	m.configMutex.Lock()
	m.config.Gamma = gamma
	err := m.config.Validate()
	m.configMutex.Unlock()

	if err != nil {
		return err
	}
	m.triggerUpdate()
	return nil
}

func (m *Manager) SetEnabled(enabled bool) {
	m.configMutex.Lock()
	m.config.Enabled = enabled
	m.configMutex.Unlock()

	if enabled {
		if !m.controlsInitialized {
			m.post(func() {
				log.Info("Creating gamma controls")
				gammaMgr := m.gammaControl.(*wlr_gamma_control.ZwlrGammaControlManagerV1)
				if err := m.setupOutputControls(m.availableOutputs, gammaMgr, false); err != nil {
					log.Errorf("Failed to create gamma controls: %v", err)
				} else {
					m.controlsInitialized = true
				}
			})
		} else {
			m.triggerUpdate()
		}
	} else {
		if m.controlsInitialized {
			const identityTemp = 6500
			log.Infof("Disabling: transitioning to %dK before destroying controls", identityTemp)
			m.startTransition(identityTemp)
		}
	}
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
		if control, ok := out.gammaControl.(*wlr_gamma_control.ZwlrGammaControlV1); ok {
			control.Destroy()
		}
	}
	m.outputs = make(map[uint32]*outputState)
	m.outputsMutex.Unlock()

	if manager, ok := m.gammaControl.(*wlr_gamma_control.ZwlrGammaControlManagerV1); ok {
		manager.Destroy()
	}

	if m.display != nil {
		m.display.Context().Close()
	}
}

func MemfdCreate(name string, flags int) (int, error) {
	fd, err := unix.MemfdCreate(name, flags)
	if err != nil {
		return -1, err
	}
	return fd, nil
}
