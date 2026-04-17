// Copyright 2014 Team 254. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)
//
// Shared interface and constants for configuring network switches for team VLANs.

package network

import (
	"github.com/Team254/cheesy-arena-lite/model"
)

const (
	red1Vlan  = 10
	red2Vlan  = 20
	red3Vlan  = 30
	blue1Vlan = 40
	blue2Vlan = 50
	blue3Vlan = 60
)

// TeamSwitch is the interface that both the Cisco and Aruba switch implementations satisfy.
type TeamSwitch interface {
	ConfigureTeamEthernet(teams [6]*model.Team) error
}

var ServerIpAddress = "10.0.100.5" // The DS will try to connect to this address only.

// NewTeamSwitch creates a TeamSwitch implementation based on the given switch type.
func NewTeamSwitch(switchType, address, username, password string) TeamSwitch {
	switch switchType {
	case "aruba2920":
		return NewArubaSwitch(address, username, password)
	default:
		return NewCiscoSwitch(address, password)
	}
}
