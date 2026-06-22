package network

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/Team254/cheesy-arena-lite/model"
	"io"
	"net/http"
	"strconv"
	"time"
)

const (
	DefaultVh113Channel  = 13
	vh113ChannelBandwidth = "40MHz"
	vh113RedVlans         = "10_20_30"
	vh113BlueVlans        = "40_50_60"
)

var vh113AllowedChannels = map[int]struct{}{
	5: {}, 13: {}, 21: {}, 29: {}, 37: {}, 45: {}, 53: {}, 61: {}, 69: {}, 77: {},
	85: {}, 93: {}, 101: {}, 109: {}, 117: {},
}

type vh113Configuration struct {
	BlueVlans             string                               `json:"blueVlans"`
	Channel               int                                  `json:"channel"`
	ChannelBandwidth      string                               `json:"channelBandwidth"`
	RedVlans              string                               `json:"redVlans"`
	StationConfigurations map[string]vh113StationConfiguration `json:"stationConfigurations"`
}

type vh113StationConfiguration struct {
	Ssid   string `json:"ssid"`
	WpaKey string `json:"wpaKey"`
}

type vh113StatusResponse struct {
	StationStatuses map[string]vh113StationStatus `json:"stationStatuses"`
}

type vh113StationStatus struct {
	Ssid     string `json:"ssid"`
	IsLinked bool   `json:"isLinked"`
}

func IsValidVh113Channel(channel int) bool {
	_, ok := vh113AllowedChannels[channel]
	return ok
}

func generateVh113Configuration(teams [6]*model.Team, channel int) ([]byte, error) {
	if !IsValidVh113Channel(channel) {
		channel = DefaultVh113Channel
	}

	stationTeams := map[string]*model.Team{
		"red1":  teams[0],
		"red2":  teams[1],
		"red3":  teams[2],
		"blue1": teams[3],
		"blue2": teams[4],
		"blue3": teams[5],
	}
	stationConfigurations := make(map[string]vh113StationConfiguration, len(stationTeams))
	for station, team := range stationTeams {
		ssid, wpaKey, err := teamWifiCredentials(team, station)
		if err != nil {
			return nil, err
		}
		stationConfigurations[station] = vh113StationConfiguration{ssid, wpaKey}
	}

	return json.Marshal(vh113Configuration{
		BlueVlans:             vh113BlueVlans,
		Channel:               channel,
		ChannelBandwidth:      vh113ChannelBandwidth,
		RedVlans:              vh113RedVlans,
		StationConfigurations: stationConfigurations,
	})
}

func decodeVh113Status(statusJson string, statuses []TeamWifiStatus) error {
	var response vh113StatusResponse
	if err := json.Unmarshal([]byte(statusJson), &response); err != nil {
		return err
	}

	for i, station := range []string{"red1", "red2", "red3", "blue1", "blue2", "blue3"} {
		status, ok := response.StationStatuses[station]
		if !ok {
			return fmt.Errorf("missing station status for %s", station)
		}
		statuses[i].TeamId, _ = strconv.Atoi(status.Ssid)
		statuses[i].RadioLinked = status.IsLinked
	}

	return nil
}

func teamWifiCredentials(team *model.Team, station string) (string, string, error) {
	if team == nil {
		placeholder := fmt.Sprintf("no-team-%s", station)
		return placeholder, placeholder, nil
	}
	if len(team.WpaKey) < 8 || len(team.WpaKey) > 63 {
		return "", "", fmt.Errorf("Invalid WPA key '%s' configured for team %d.", team.WpaKey, team.Id)
	}
	return strconv.Itoa(team.Id), team.WpaKey, nil
}

func (ap *AccessPoint) configureVh113TeamWifi(teams [6]*model.Team) error {
	config, err := generateVh113Configuration(teams, ap.teamChannel)
	if err != nil {
		return err
	}

	request, err := http.NewRequest("POST", fmt.Sprintf("http://%s/configuration", ap.address), bytes.NewReader(config))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := (&http.Client{Timeout: accessPointCommandTimeoutSec * time.Second}).Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(response.Body)
		return fmt.Errorf("VH-113 configuration request failed with status %d: %s", response.StatusCode, string(body))
	}

	return nil
}

func (ap *AccessPoint) updateVh113TeamWifiStatuses() error {
	response, err := (&http.Client{Timeout: accessPointCommandTimeoutSec * time.Second}).Get(
		fmt.Sprintf("http://%s/status", ap.address),
	)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		return fmt.Errorf("VH-113 status request failed with status %d: %s", response.StatusCode, string(body))
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}

	return decodeVh113Status(string(body), ap.TeamWifiStatuses[:])
}