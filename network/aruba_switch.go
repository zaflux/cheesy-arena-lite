// Copyright 2026 Team 1732. All Rights Reserved.
//
// Methods for configuring an HP/Aruba 2920-series switch for team VLANs via SSH.

package network

import (
	"bytes"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/Team254/cheesy-arena-lite/model"
	"golang.org/x/crypto/ssh"
)

const (
	arubaSwitchSSHPort           = 22
	arubaSwitchConnectTimeoutSec = 5
	arubaSwitchConfigTimeoutSec  = 10
	arubaSwitchConfigBackoffSec  = 5
	arubaSwitchConfigPauseSec    = 2
	arubaSwitchTeamGateway       = 61
)

type ArubaSwitch struct {
	address                string
	port                   int
	username               string
	password               string
	mutex                  sync.Mutex
	connectTimeoutDuration time.Duration
	configTimeoutDuration  time.Duration
	configBackoffDuration  time.Duration
	configPauseDuration    time.Duration
}

func NewArubaSwitch(address, username, password string) *ArubaSwitch {
	return &ArubaSwitch{
		address:                address,
		port:                   arubaSwitchSSHPort,
		username:               username,
		password:               password,
		connectTimeoutDuration: arubaSwitchConnectTimeoutSec * time.Second,
		configTimeoutDuration:  arubaSwitchConfigTimeoutSec * time.Second,
		configBackoffDuration:  arubaSwitchConfigBackoffSec * time.Second,
		configPauseDuration:    arubaSwitchConfigPauseSec * time.Second,
	}
}

// Sets up wired networks for the given set of teams.
func (sw *ArubaSwitch) ConfigureTeamEthernet(teams [6]*model.Team) error {
	sw.mutex.Lock()
	defer sw.mutex.Unlock()

	// Remove old team VLANs and DHCP pools to reset the switch state.
	removeCommands := []string{"configure terminal"}
	for vlan := 10; vlan <= 60; vlan += 10 {
		removeCommands = append(removeCommands,
			fmt.Sprintf("no dhcp-server pool \"dhcp%d\"", vlan),
			fmt.Sprintf("vlan %d", vlan),
			"no ip address",
			"exit",
		)
	}
	removeCommands = append(removeCommands, "exit")

	_, err := sw.runCommandSequence(removeCommands)
	if err != nil {
		return err
	}
	time.Sleep(sw.configPauseDuration)

	// Create the new team VLANs and DHCP pools.
	addCommands := []string{"configure terminal"}
	hasTeams := false

	addTeamVlan := func(team *model.Team, vlan int) {
		if team == nil {
			return
		}
		hasTeams = true
		teamPartialIp := fmt.Sprintf("%d.%d", team.Id/100, team.Id%100)
		gatewayIp := fmt.Sprintf("10.%s.%d", teamPartialIp, arubaSwitchTeamGateway)
		network := fmt.Sprintf("10.%s.0", teamPartialIp)

		// Configure the VLAN interface IP.
		addCommands = append(addCommands,
			fmt.Sprintf("vlan %d", vlan),
			fmt.Sprintf("ip address %s 255.255.255.0", gatewayIp),
			fmt.Sprintf("ip access-group \"%d\" in", 100+vlan),
			"exit",
		)

		// Configure the access list for this team.
		addCommands = append(addCommands,
			fmt.Sprintf("ip access-list extended \"%d\"", 100+vlan),
			fmt.Sprintf("10 permit ip 10.%s.0 0.0.0.255 host %s", teamPartialIp, ServerIpAddress),
			"20 permit udp any eq 68 any eq 67",
			"30 deny ip any any",
			"exit",
		)

		// Configure the DHCP pool for this team.
		// Aruba uses explicit ranges rather than exclusions.
		// Usable range: 10.x.y.101 through 10.x.y.199
		addCommands = append(addCommands,
			fmt.Sprintf("dhcp-server pool \"dhcp%d\"", vlan),
			fmt.Sprintf("network %s 255.255.255.0", network),
			fmt.Sprintf("range 10.%s.101 10.%s.199", teamPartialIp, teamPartialIp),
			fmt.Sprintf("default-router %s", gatewayIp),
			"lease 7",
			"exit",
		)
	}

	addTeamVlan(teams[0], red1Vlan)
	addTeamVlan(teams[1], red2Vlan)
	addTeamVlan(teams[2], red3Vlan)
	addTeamVlan(teams[3], blue1Vlan)
	addTeamVlan(teams[4], blue2Vlan)
	addTeamVlan(teams[5], blue3Vlan)

	if hasTeams {
		addCommands = append(addCommands, "write memory", "exit")
		_, err = sw.runCommandSequence(addCommands)
		if err != nil {
			return err
		}
	}

	// Give some time for the configuration to take before another one can be attempted.
	time.Sleep(sw.configBackoffDuration)

	return nil
}

// Logs into the switch via SSH and runs the given commands in sequence.
func (sw *ArubaSwitch) runCommandSequence(commands []string) (string, error) {
	sshConfig := &ssh.ClientConfig{
		User: sw.username,
		Auth: []ssh.AuthMethod{
			ssh.Password(sw.password),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         sw.connectTimeoutDuration,
	}

	client, err := ssh.Dial("tcp", net.JoinHostPort(sw.address, strconv.Itoa(sw.port)), sshConfig)
	if err != nil {
		return "", fmt.Errorf("failed to connect to Aruba switch via SSH: %w", err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("failed to create SSH session: %w", err)
	}
	defer session.Close()

	var outputBuffer bytes.Buffer
	session.Stdout = &outputBuffer
	session.Stderr = &outputBuffer

	inputPipe, err := session.StdinPipe()
	if err != nil {
		return "", fmt.Errorf("failed to create input pipe: %w", err)
	}

	modes := ssh.TerminalModes{ssh.ECHO: 0}
	if err := session.RequestPty("vt100", 80, 40, modes); err != nil {
		return "", fmt.Errorf("failed to configure shell: %w", err)
	}

	err = session.Shell()
	if err != nil {
		return "", fmt.Errorf("failed to start shell: %w", err)
	}

	for _, command := range commands {
		if _, err := fmt.Fprintln(inputPipe, command); err != nil {
			return "", fmt.Errorf("failed to write command to switch: %w", err)
		}
	}

	// Close the input to signal we're done sending commands.
	inputPipe.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.Wait()
	}()
	select {
	case err := <-done:
		if err != nil {
			return "", fmt.Errorf("failed to run command sequence: %w", err)
		}
	case <-time.After(sw.configTimeoutDuration):
		return "", fmt.Errorf("timed out waiting for command sequence to complete")
	}

	return outputBuffer.String(), nil
}
