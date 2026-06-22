// Copyright 2017 Team 254. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)

package network

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Team254/cheesy-arena-lite/model"
	"github.com/stretchr/testify/assert"
)

func TestVH113SetSettings(t *testing.T) {
	ap := VH113AccessPoint{}

	// username, adminChannel, and adminWpaKey are accepted but ignored.
	ap.SetSettings("10.0.100.2", "ignored-user", "secretpass", 93, 0, "ignored-key", true)
	assert.Equal(t, "http://10.0.100.2:8081", ap.apiUrl)
	assert.Equal(t, "secretpass", ap.password)
	assert.Equal(t, 93, ap.teamChannel)
	assert.True(t, ap.networkSecurityEnabled)
}

func TestVH113ConfigureTeamWifi(t *testing.T) {
	teams := [6]*model.Team{
		{Id: 254, WpaKey: "aaaaaaaa"},
		nil,
		{Id: 1678, WpaKey: "bbbbbbbb"},
		nil,
		{Id: 2910, WpaKey: "cccccccc"},
		nil,
	}

	var capturedRequest vh113ConfigurationRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/configuration", r.URL.Path)
		assert.Equal(t, "Bearer testtoken", r.Header.Get("Authorization"))
		if err := json.NewDecoder(r.Body).Decode(&capturedRequest); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ap := VH113AccessPoint{}
	ap.apiUrl = server.URL
	ap.password = "testtoken"
	ap.teamChannel = 157
	ap.networkSecurityEnabled = true

	err := ap.ConfigureTeamWifi(teams)
	assert.Nil(t, err)

	// Verify the request body.
	assert.Equal(t, 157, capturedRequest.Channel)
	assert.Equal(t, 3, len(capturedRequest.StationConfigurations))
	assert.Equal(t, "254", capturedRequest.StationConfigurations["red1"].Ssid)
	assert.Equal(t, "aaaaaaaa", capturedRequest.StationConfigurations["red1"].WpaKey)
	assert.Equal(t, "1678", capturedRequest.StationConfigurations["red3"].Ssid)
	assert.Equal(t, "bbbbbbbb", capturedRequest.StationConfigurations["red3"].WpaKey)
	assert.Equal(t, "2910", capturedRequest.StationConfigurations["blue2"].Ssid)
	assert.Equal(t, "cccccccc", capturedRequest.StationConfigurations["blue2"].WpaKey)

	// nil teams should not appear in the configuration.
	_, hasRed2 := capturedRequest.StationConfigurations["red2"]
	_, hasBlue1 := capturedRequest.StationConfigurations["blue1"]
	_, hasBlue3 := capturedRequest.StationConfigurations["blue3"]
	assert.False(t, hasRed2)
	assert.False(t, hasBlue1)
	assert.False(t, hasBlue3)
}

func TestVH113ConfigureTeamWifiNoSecurityEnabled(t *testing.T) {
	ap := VH113AccessPoint{}
	ap.networkSecurityEnabled = false
	// Should be a no-op and not make any HTTP requests.
	err := ap.ConfigureTeamWifi([6]*model.Team{})
	assert.Nil(t, err)
}

func TestVH113ConfigureTeamWifiInvalidWpaKey(t *testing.T) {
	ap := VH113AccessPoint{}
	ap.networkSecurityEnabled = true
	ap.apiUrl = "http://localhost:0" // unreachable but error occurs before HTTP call

	teams := [6]*model.Team{{Id: 254, WpaKey: "short"}, nil, nil, nil, nil, nil}
	err := ap.ConfigureTeamWifi(teams)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "invalid WPA key")
}

func TestVH113ConfigureTeamWifiServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer server.Close()

	ap := VH113AccessPoint{}
	ap.apiUrl = server.URL
	ap.teamChannel = 157
	ap.networkSecurityEnabled = true

	teams := [6]*model.Team{{Id: 254, WpaKey: "aaaaaaaa"}, nil, nil, nil, nil, nil}
	err := ap.ConfigureTeamWifi(teams)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestVH113ConfigureAdminWifi(t *testing.T) {
	ap := VH113AccessPoint{}
	// Should always succeed without doing anything.
	assert.Nil(t, ap.ConfigureAdminWifi())
}

func TestVH113GetTeamWifiStatuses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/status", r.URL.Path)
		resp := vh113StatusResponse{
			Channel: 157,
			Status:  "ACTIVE",
			StationStatuses: map[string]*vh113StationStatus{
				"red1":  {Ssid: "254", IsLinked: true},
				"red2":  {Ssid: "1678", IsLinked: false},
				"red3":  nil,
				"blue1": {Ssid: "2910", IsLinked: true},
				"blue2": nil,
				"blue3": {Ssid: "604", IsLinked: false},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	ap := VH113AccessPoint{}
	ap.apiUrl = server.URL
	ap.networkSecurityEnabled = true

	err := ap.updateStatus()
	assert.Nil(t, err)
	assert.Equal(t, "ACTIVE", ap.status)

	statuses := ap.GetTeamWifiStatuses()
	assertTeamWifiStatus(t, 254, true, statuses[0])
	assertTeamWifiStatus(t, 1678, false, statuses[1])
	assertTeamWifiStatus(t, 0, false, statuses[2])
	assertTeamWifiStatus(t, 2910, true, statuses[3])
	assertTeamWifiStatus(t, 0, false, statuses[4])
	assertTeamWifiStatus(t, 604, false, statuses[5])
}

func TestVH113StatusMatchesLastConfiguration(t *testing.T) {
	ap := VH113AccessPoint{}
	teams := [6]*model.Team{
		{Id: 254, WpaKey: "aaaaaaaa"},
		nil,
		nil,
		nil,
		nil,
		{Id: 1114, WpaKey: "bbbbbbbb"},
	}
	ap.lastConfiguredTeams = teams
	ap.teamWifiStatuses = [6]TeamWifiStatus{
		{TeamId: 254},
		{TeamId: 0},
		{TeamId: 0},
		{TeamId: 0},
		{TeamId: 0},
		{TeamId: 1114},
	}
	assert.True(t, ap.statusMatchesLastConfiguration())

	// Mismatch: wrong team in slot 5.
	ap.teamWifiStatuses[5] = TeamWifiStatus{TeamId: 999}
	assert.False(t, ap.statusMatchesLastConfiguration())
}

func TestNewAccessPoint(t *testing.T) {
	// Linksys (default)
	ap := NewAccessPoint("linksys")
	_, isLinksys := ap.(*LinksysAccessPoint)
	assert.True(t, isLinksys)
	assert.Equal(t, "linksys", AccessPointType(ap))

	// VH-113
	ap = NewAccessPoint("vh113")
	_, isVH113 := ap.(*VH113AccessPoint)
	assert.True(t, isVH113)
	assert.Equal(t, "vh113", AccessPointType(ap))

	// Unknown type falls back to Linksys.
	ap = NewAccessPoint("unknown")
	_, isLinksys = ap.(*LinksysAccessPoint)
	assert.True(t, isLinksys)
	assert.Equal(t, "linksys", AccessPointType(ap))
}
