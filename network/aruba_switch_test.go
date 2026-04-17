// Copyright 2026 Team 1732. All Rights Reserved.
//
// Tests for the Aruba 2920 switch configuration.

package network

import (
	"testing"
	"time"

	"github.com/Team254/cheesy-arena-lite/model"
	"github.com/stretchr/testify/assert"
)

func TestNewArubaSwitch(t *testing.T) {
	sw := NewArubaSwitch("10.0.100.3", "manager", "password")
	assert.Equal(t, "10.0.100.3", sw.address)
	assert.Equal(t, 22, sw.port)
	assert.Equal(t, "manager", sw.username)
	assert.Equal(t, "password", sw.password)
}

func TestArubaSwitchInterfaceCompliance(t *testing.T) {
	// Verify that ArubaSwitch satisfies the TeamSwitch interface at compile time.
	var _ TeamSwitch = (*ArubaSwitch)(nil)
}

func TestCiscoSwitchInterfaceCompliance(t *testing.T) {
	// Verify that CiscoSwitch satisfies the TeamSwitch interface at compile time.
	var _ TeamSwitch = (*CiscoSwitch)(nil)
}

func TestArubaSwitchConfigureTeamEthernetNoTeams(t *testing.T) {
	sw := NewArubaSwitch("127.0.0.1", "manager", "password")
	sw.configBackoffDuration = time.Millisecond
	sw.configPauseDuration = time.Millisecond

	// Without a real SSH server, this will fail to connect, which is expected.
	err := sw.ConfigureTeamEthernet([6]*model.Team{nil, nil, nil, nil, nil, nil})
	assert.NotNil(t, err)
}
