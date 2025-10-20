package wayland

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/AvengeMedia/danklinux/internal/log"
)

type ipInfoResponse struct {
	Loc  string `json:"loc"`
	City string `json:"city"`
}

func FetchIPLocation() (*float64, *float64, error) {
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.Get("http://ipinfo.io/json")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch IP location: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("ipinfo.io returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read response: %w", err)
	}

	var data ipInfoResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if data.Loc == "" {
		return nil, nil, fmt.Errorf("missing location data in response")
	}

	parts := strings.Split(data.Loc, ",")
	if len(parts) != 2 {
		return nil, nil, fmt.Errorf("invalid coordinates format: %s", data.Loc)
	}

	lat, err := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid latitude: %w", err)
	}

	lon, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid longitude: %w", err)
	}

	log.Infof("Fetched IP-based location: %s (%.4f, %.4f)", data.City, lat, lon)
	return &lat, &lon, nil
}
