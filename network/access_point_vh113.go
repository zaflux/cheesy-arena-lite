// Copyright 2017 Team 254. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)
//
// Methods for configuring a Vivid-Hosting VH-113 access point via its HTTP REST API
// (https://github.com/patfair/frc-radio-api).

package network

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/Team254/cheesy-arena-lite/model"
)

const (
	vh113ApiPort        = 8081
	vh113PollPeriodSec  = 3
	vh113HttpTimeoutSec = 3
)

// VH113AccessPoint configures a Vivid-Hosting VH-113 access point via its HTTP REST API.
type VH113AccessPoint struct {
	apiUrl                 string
	password               string
	teamChannel            int
	networkSecurityEnabled bool
	teamWifiStatuses       [6]TeamWifiStatus
	lastConfiguredTeams    [6]*model.Team
	status                 string
}

// vh113ConfigurationRequest is the JSON body for POST /configuration.
type vh113ConfigurationRequest struct {
	Channel               int                              `json:"channel"`
	StationConfigurations map[string]vh113StationConfig    `json:"stationConfigurations"`
}

type vh113StationConfig struct {
	Ssid   string `json:"ssid"`
	WpaKey string `json:"wpaKey"`
}

// vh113StatusResponse is the JSON body returned by GET /status.
type vh113StatusResponse struct {
	Channel         int                         `json:"channel"`
	Status          string                      `json:"status"`
	StationStatuses map[string]*vh113StationStatus `json:"stationStatuses"`
}

type vh113StationStatus struct {
	Ssid     string `json:"ssid"`
	IsLinked bool   `json:"isLinked"`
}

// SetSettings stores the AP settings. The username, adminChannel, and adminWpaKey parameters are
// accepted for interface compatibility but ignored by the VH-113 (which uses Bearer token auth and
// has no separate admin radio).
func (ap *VH113AccessPoint) SetSettings(address, username, password string, teamChannel, adminChannel int,
	adminWpaKey string, networkSecurityEnabled bool) {
	ap.apiUrl = fmt.Sprintf("http://%s:%d", address, vh113ApiPort)
	ap.password = password
	ap.teamChannel = teamChannel
	ap.networkSecurityEnabled = networkSecurityEnabled
	if ap.status == "" {
		ap.status = "UNKNOWN"
	}
}

// GetTeamWifiStatuses returns a copy of the current team wifi statuses.
func (ap *VH113AccessPoint) GetTeamWifiStatuses() [6]TeamWifiStatus {
	return ap.teamWifiStatuses
}

// Run loops indefinitely, polling the AP status and retrying configuration when needed.
func (ap *VH113AccessPoint) Run() {
	for {
		time.Sleep(time.Second * vh113PollPeriodSec)
		if !ap.networkSecurityEnabled {
			continue
		}
		if err := ap.updateStatus(); err != nil {
			log.Printf("Failed to update VH-113 status: %v", err)
			continue
		}
		// If the AP is active but its SSIDs don't match what was last configured, retry.
		if ap.status == "ACTIVE" && !ap.statusMatchesLastConfiguration() {
			log.Println("VH-113 is ACTIVE but does not match expected configuration; retrying.")
			if err := ap.sendConfiguration(ap.lastConfiguredTeams); err != nil {
				log.Printf("Failed to reconfigure VH-113: %v", err)
			}
		}
	}
}

// ConfigureTeamWifi sends a new configuration to the VH-113 via its REST API.
func (ap *VH113AccessPoint) ConfigureTeamWifi(teams [6]*model.Team) error {
	if !ap.networkSecurityEnabled {
		return nil
	}
	return ap.sendConfiguration(teams)
}

// ConfigureAdminWifi is a no-op for the VH-113, which has no separate admin radio.
func (ap *VH113AccessPoint) ConfigureAdminWifi() error {
	return nil
}

// sendConfiguration POSTs the channel and team station configurations to the VH-113 API.
func (ap *VH113AccessPoint) sendConfiguration(teams [6]*model.Team) error {
	ap.status = "CONFIGURING"
	ap.lastConfiguredTeams = teams

	stationNames := []string{"red1", "red2", "red3", "blue1", "blue2", "blue3"}

	reqBody := vh113ConfigurationRequest{
		Channel:               ap.teamChannel,
		StationConfigurations: make(map[string]vh113StationConfig),
	}
	for i, team := range teams {
		if team != nil {
			if len(team.WpaKey) < 8 || len(team.WpaKey) > 63 {
				return fmt.Errorf("invalid WPA key '%s' configured for team %d", team.WpaKey, team.Id)
			}
			reqBody.StationConfigurations[stationNames[i]] = vh113StationConfig{
				Ssid:   strconv.Itoa(team.Id),
				WpaKey: team.WpaKey,
			}
		}
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	url := ap.apiUrl + "/configuration"
	httpReq, err := http.NewRequest("POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if ap.password != "" {
		httpReq.Header.Set("Authorization", "Bearer "+ap.password)
	}

	client := &http.Client{Timeout: time.Second * vh113HttpTimeoutSec}
	resp, err := client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("VH-113 configuration request failed: %v", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Printf("Failed to close VH-113 configuration response body: %v", closeErr)
		}
	}()

	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("VH-113 returned status %d: %s", resp.StatusCode, string(body))
	}

	log.Println("VH-113 accepted the new configuration and will apply it asynchronously.")
	return nil
}

// updateStatus fetches the current AP status from GET /status and updates teamWifiStatuses.
func (ap *VH113AccessPoint) updateStatus() error {
	url := ap.apiUrl + "/status"
	httpReq, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	if ap.password != "" {
		httpReq.Header.Set("Authorization", "Bearer "+ap.password)
	}

	client := &http.Client{Timeout: time.Second * vh113HttpTimeoutSec}
	resp, err := client.Do(httpReq)
	if err != nil {
		ap.status = "ERROR"
		return fmt.Errorf("failed to fetch VH-113 status: %v", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Printf("Failed to close VH-113 status response body: %v", closeErr)
		}
	}()

	if resp.StatusCode/100 != 2 {
		ap.status = "ERROR"
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("VH-113 returned status %d: %s", resp.StatusCode, string(body))
	}

	var statusResp vh113StatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&statusResp); err != nil {
		ap.status = "ERROR"
		return fmt.Errorf("failed to parse VH-113 status: %v", err)
	}

	if ap.status != statusResp.Status {
		log.Printf("VH-113 status changed from %s to %s.", ap.status, statusResp.Status)
		ap.status = statusResp.Status
	}

	stationNames := []string{"red1", "red2", "red3", "blue1", "blue2", "blue3"}
	for i, name := range stationNames {
		station := statusResp.StationStatuses[name]
		if station == nil {
			ap.teamWifiStatuses[i] = TeamWifiStatus{}
		} else {
			teamId, err := strconv.Atoi(station.Ssid)
			if err != nil {
				log.Printf("Failed to parse VH-113 station %s SSID %q as team ID: %v", name, station.Ssid, err)
			}
			ap.teamWifiStatuses[i] = TeamWifiStatus{
				TeamId:      teamId,
				RadioLinked: station.IsLinked,
			}
		}
	}

	return nil
}

// statusMatchesLastConfiguration returns true if the current AP SSIDs match lastConfiguredTeams.
func (ap *VH113AccessPoint) statusMatchesLastConfiguration() bool {
	for i, team := range ap.lastConfiguredTeams {
		var expectedId int
		if team != nil {
			expectedId = team.Id
		}
		if ap.teamWifiStatuses[i].TeamId != expectedId {
			return false
		}
	}
	return true
}
