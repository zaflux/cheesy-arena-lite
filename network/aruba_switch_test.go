// Copyright 2026 Team 1732. All Rights Reserved.
//
// Tests for the Aruba 2920 switch configuration.

package network

import (
	"strings"
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

func TestArubaSwitchBuildConfigureCommands_NoTeams(t *testing.T) {
	sw := NewArubaSwitch("10.0.100.3", "manager", "password")

	removeCommands, addCommands, hasTeams := sw.buildConfigureCommands([6]*model.Team{})

	assert.False(t, hasTeams)
	assert.Contains(t, removeCommands, "configure terminal")
	assert.Contains(t, removeCommands, "no dhcp-server pool \"dhcp10\"")
	assert.Contains(t, removeCommands, "vlan 10")
	assert.Contains(t, removeCommands, "no dhcp-server")
	assert.Contains(t, removeCommands, "no ip access-group \"110\" vlan-in")
	assert.Contains(t, removeCommands, "no ip address")
	assert.Contains(t, removeCommands, "no ip access-list extended \"110\"")
	assert.Contains(t, removeCommands, "vlan 100")
	assert.Contains(t, removeCommands, "dhcp-server")
	assert.Equal(t, []string{"configure terminal", "vlan 100", "dhcp-server", "exit"}, addCommands)
}

func TestArubaSwitchBuildConfigureCommands_TeamVlanDhcpAndAcl(t *testing.T) {
	sw := NewArubaSwitch("10.0.100.3", "manager", "password")

	teams := [6]*model.Team{nil, &model.Team{Id: 254}, nil, nil, nil, nil}
	_, addCommands, hasTeams := sw.buildConfigureCommands(teams)

	assert.True(t, hasTeams)
	commandText := strings.Join(addCommands, "\n")

	assert.Contains(t, commandText, "vlan 20")
	assert.Contains(t, commandText, "ip address 10.2.54.61 255.255.255.0")
	assert.Contains(t, commandText, "dhcp-server")
	assert.Contains(t, commandText, "ip access-group \"120\" vlan-in")
	assert.Contains(t, commandText, "ip access-list extended \"120\"")
	assert.Contains(t, commandText, "10 permit ip 10.2.54.0 0.0.0.255 host 10.0.100.5")
	assert.Contains(t, commandText, "dhcp-server pool \"dhcp20\"")
	assert.Contains(t, commandText, "authoritative")
	assert.Contains(t, commandText, "network 10.2.54.0 255.255.255.0")
	assert.Contains(t, commandText, "range 10.2.54.101 10.2.54.199")
	assert.Contains(t, commandText, "default-router 10.2.54.61")
	assert.Contains(t, commandText, "vlan 100")
}

func TestArubaSwitchBuildConfigureCommands_10000SeriesTeamAddressing(t *testing.T) {
	sw := NewArubaSwitch("10.0.100.3", "manager", "password")

	teams := [6]*model.Team{&model.Team{Id: 10173}, nil, nil, nil, nil, nil}
	_, addCommands, hasTeams := sw.buildConfigureCommands(teams)

	assert.True(t, hasTeams)
	commandText := strings.Join(addCommands, "\n")
	assert.Contains(t, commandText, "ip address 10.101.73.61 255.255.255.0")
	assert.Contains(t, commandText, "network 10.101.73.0 255.255.255.0")
	assert.Contains(t, commandText, "range 10.101.73.101 10.101.73.199")
	assert.Contains(t, commandText, "default-router 10.101.73.61")
}
