package network

import (
	"encoding/json"
	"github.com/Team254/cheesy-arena-lite/model"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestGenerateVh113Configuration(t *testing.T) {
	configBytes, err := generateVh113Configuration([6]*model.Team{
		{Id: 111, WpaKey: "11111111"},
		nil,
		{Id: 333, WpaKey: "33333333"},
		{Id: 222, WpaKey: "22222222"},
		nil,
		{Id: 666, WpaKey: "66666666"},
	}, 45)
	if !assert.Nil(t, err) {
		return
	}

	var config vh113Configuration
	if !assert.Nil(t, json.Unmarshal(configBytes, &config)) {
		return
	}

	assert.Equal(t, 45, config.Channel)
	assert.Equal(t, vh113ChannelBandwidth, config.ChannelBandwidth)
	assert.Equal(t, vh113RedVlans, config.RedVlans)
	assert.Equal(t, vh113BlueVlans, config.BlueVlans)
	assert.Equal(t, vh113StationConfiguration{Ssid: "111", WpaKey: "11111111"}, config.StationConfigurations["red1"])
	assert.Equal(t, vh113StationConfiguration{Ssid: "no-team-red2", WpaKey: "no-team-red2"}, config.StationConfigurations["red2"])
	assert.Equal(t, vh113StationConfiguration{Ssid: "333", WpaKey: "33333333"}, config.StationConfigurations["red3"])
	assert.Equal(t, vh113StationConfiguration{Ssid: "222", WpaKey: "22222222"}, config.StationConfigurations["blue1"])
	assert.Equal(t, vh113StationConfiguration{Ssid: "no-team-blue2", WpaKey: "no-team-blue2"}, config.StationConfigurations["blue2"])
	assert.Equal(t, vh113StationConfiguration{Ssid: "666", WpaKey: "66666666"}, config.StationConfigurations["blue3"])

	_, err = generateVh113Configuration([6]*model.Team{{Id: 254, WpaKey: "short"}, nil, nil, nil, nil, nil}, 45)
	assert.NotNil(t, err)
}

func TestDecodeVh113Status(t *testing.T) {
	var statuses [6]TeamWifiStatus
	err := decodeVh113Status(`{"stationStatuses":{"red1":{"ssid":"111","isLinked":true},"red2":{"ssid":"no-team-red2","isLinked":false},"red3":{"ssid":"333","isLinked":false},"blue1":{"ssid":"222","isLinked":true},"blue2":{"ssid":"no-team-blue2","isLinked":false},"blue3":{"ssid":"666","isLinked":true}}}`, statuses[:])
	if !assert.Nil(t, err) {
		return
	}

	assert.Equal(t, TeamWifiStatus{TeamId: 111, RadioLinked: true}, statuses[0])
	assert.Equal(t, TeamWifiStatus{TeamId: 0, RadioLinked: false}, statuses[1])
	assert.Equal(t, TeamWifiStatus{TeamId: 333, RadioLinked: false}, statuses[2])
	assert.Equal(t, TeamWifiStatus{TeamId: 222, RadioLinked: true}, statuses[3])
	assert.Equal(t, TeamWifiStatus{TeamId: 0, RadioLinked: false}, statuses[4])
	assert.Equal(t, TeamWifiStatus{TeamId: 666, RadioLinked: true}, statuses[5])

	err = decodeVh113Status(`{"stationStatuses":{"red1":{"ssid":"111","isLinked":true}}}`, statuses[:])
	assert.NotNil(t, err)
}