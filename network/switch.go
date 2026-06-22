// Copyright 2014 Team 254. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)
//
// Shared interface and constants for configuring network switches for team VLANs.

package network

import (
	"fmt"
	"sync/atomic"

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

var switchDiagnosticLoggingEnabled atomic.Bool

func SetSwitchDiagnosticLoggingEnabled(enabled bool) {
	switchDiagnosticLoggingEnabled.Store(enabled)
}

func isSwitchDiagnosticLoggingEnabled() bool {
	return switchDiagnosticLoggingEnabled.Load()
}

func teamSubnetOctets(teamID int) (int, int) {
	return teamID / 100, teamID % 100
}

func teamSubnetPrefix(teamID int) string {
	secondOctet, thirdOctet := teamSubnetOctets(teamID)
	return fmt.Sprintf("10.%d.%d", secondOctet, thirdOctet)
}

func teamIDFromSubnetOctets(secondOctet, thirdOctet int) int {
	return secondOctet*100 + thirdOctet
}

// NewTeamSwitch creates a TeamSwitch implementation based on the given switch type.
func NewTeamSwitch(switchType, address, username, password string) TeamSwitch {
	switch switchType {
	case "aruba2920":
		return NewArubaSwitchRest(address, username, password)
	case "aruba2920batch":
		return NewArubaSwitchRestBatch(address, username, password)
	case "aruba2920ssh":
		return NewArubaSwitch(address, username, password)
	default:
		return NewCiscoSwitch(address, password)
	}
}
