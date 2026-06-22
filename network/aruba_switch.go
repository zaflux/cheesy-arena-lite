// Copyright 2026 Team 1732. All Rights Reserved.
//
// Methods for configuring an HP/Aruba 2920-series switch for team VLANs via SSH.

package network

import (
	"bytes"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
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
	arubaSwitchTeamGateway       = 4
	arubaSwitchShellStartPauseMs = 250
	arubaSwitchCommandPaceMs     = 40
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
	log.Printf("ArubaSwitch(%s): requested Ethernet reconfiguration for teams [%s]", sw.address,
		arubaTeamSummary(teams))

	removeCommands, addCommands, hasTeams := sw.buildConfigureCommands(teams)
	log.Printf("ArubaSwitch(%s): reset command block:\n%s", sw.address, formatArubaCommandBlock(removeCommands))
	if hasTeams {
		log.Printf("ArubaSwitch(%s): apply command block:\n%s", sw.address, formatArubaCommandBlock(addCommands))
	} else {
		log.Printf("ArubaSwitch(%s): no teams assigned; only reset/remove commands will be sent.", sw.address)
	}

	log.Printf("ArubaSwitch(%s): sending reset command block.", sw.address)
	output, err := sw.runCommandSequence(removeCommands)
	if err != nil {
		log.Printf("ArubaSwitch(%s): failed sending reset command block: %s", sw.address, err.Error())
		return err
	}
	if strings.TrimSpace(output) != "" {
		log.Printf("ArubaSwitch(%s): reset command output:\n%s", sw.address, output)
	}
	time.Sleep(sw.configPauseDuration)

	if hasTeams {
		log.Printf("ArubaSwitch(%s): sending apply command block.", sw.address)
		output, err = sw.runCommandSequence(addCommands)
		if err != nil {
			log.Printf("ArubaSwitch(%s): failed sending apply command block: %s", sw.address, err.Error())
			return err
		}
		if strings.TrimSpace(output) != "" {
			log.Printf("ArubaSwitch(%s): apply command output:\n%s", sw.address, output)
		}
	} else {
		// Persist removal-only changes as well.
		log.Printf("ArubaSwitch(%s): writing memory for removal-only update.", sw.address)
		_, err = sw.runCommandSequence([]string{"write memory"})
		if err != nil {
			log.Printf("ArubaSwitch(%s): failed writing memory: %s", sw.address, err.Error())
			return err
		}
	}
	log.Printf("ArubaSwitch(%s): completed Ethernet reconfiguration for teams [%s]", sw.address,
		arubaTeamSummary(teams))

	// Give some time for the configuration to take before another one can be attempted.
	time.Sleep(sw.configBackoffDuration)

	return nil
}

func (sw *ArubaSwitch) buildConfigureCommands(teams [6]*model.Team) ([]string, []string, bool) {
	// Remove old team VLANs and DHCP pools to reset the switch state.
	removeCommands := []string{"configure terminal"}
	for vlan := red1Vlan; vlan <= blue3Vlan; vlan += 10 {
		aclName := 100 + vlan
		removeCommands = append(removeCommands,
			fmt.Sprintf("no dhcp-server pool \"dhcp%d\"", vlan),
			fmt.Sprintf("vlan %d", vlan),
			"no dhcp-server",
			fmt.Sprintf("no ip access-group \"%d\" vlan-in", aclName),
			"no ip address",
			"exit",
			fmt.Sprintf("no ip access-list extended \"%d\"", aclName),
		)
	}
	removeCommands = append(removeCommands, "vlan 100", "dhcp-server", "exit")
	removeCommands = append(removeCommands, "exit")

	// Create the new team VLANs and DHCP pools.
	addCommands := []string{"configure terminal"}
	hasTeams := false

	addTeamVlan := func(team *model.Team, vlan int) {
		if team == nil {
			return
		}
		hasTeams = true
		teamSubnet := teamSubnetPrefix(team.Id)
		gatewayIp := fmt.Sprintf("%s.%d", teamSubnet, arubaSwitchTeamGateway)
		network := fmt.Sprintf("%s.0", teamSubnet)
		aclName := 100 + vlan

		// Configure the VLAN interface IP and tie DHCP service to this VLAN.
		addCommands = append(addCommands,
			fmt.Sprintf("vlan %d", vlan),
			fmt.Sprintf("ip address %s 255.255.255.0", gatewayIp),
			"dhcp-server",
			fmt.Sprintf("ip access-group \"%d\" vlan-in", aclName),
			"exit",
		)

		// Configure the access list for this team.
		addCommands = append(addCommands,
			fmt.Sprintf("ip access-list extended \"%d\"", aclName),
			fmt.Sprintf("10 permit ip %s.0 0.0.0.255 host %s", teamSubnet, ServerIpAddress),
			"20 permit udp any eq 68 any eq 67",
			"30 deny ip any any",
			"exit",
		)

		// Configure the DHCP pool for this team.
		// Aruba uses explicit ranges rather than exclusions.
		// Usable range: 10.x.y.101 through 10.x.y.199
		addCommands = append(addCommands,
			fmt.Sprintf("dhcp-server pool \"dhcp%d\"", vlan),
			"authoritative",
			fmt.Sprintf("network %s 255.255.255.0", network),
			fmt.Sprintf("range %s.101 %s.199", teamSubnet, teamSubnet),
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
	addCommands = append(addCommands, "vlan 100", "dhcp-server", "exit")

	if hasTeams {
		addCommands = append(addCommands, "write memory", "exit")
	}

	return removeCommands, addCommands, hasTeams
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
	if err := session.RequestPty("dumb", 200, 60, modes); err != nil {
		return "", fmt.Errorf("failed to configure shell: %w", err)
	}

	err = session.Shell()
	if err != nil {
		return "", fmt.Errorf("failed to start shell: %w", err)
	}

	// Allow the remote shell to fully initialize before sending commands.
	time.Sleep(arubaSwitchShellStartPauseMs * time.Millisecond)

	// Sync to a clean prompt and disable paging to avoid interactive pauses.
	if _, err := fmt.Fprint(inputPipe, "\r\n"); err != nil {
		return "", fmt.Errorf("failed to prime switch shell: %w", err)
	}
	time.Sleep(arubaSwitchCommandPaceMs * time.Millisecond)
	if _, err := fmt.Fprint(inputPipe, "no page\r\n"); err != nil {
		return "", fmt.Errorf("failed to disable paging on switch shell: %w", err)
	}
	time.Sleep(arubaSwitchCommandPaceMs * time.Millisecond)

	for _, command := range commands {
		if _, err := fmt.Fprintf(inputPipe, "%s\r\n", command); err != nil {
			return "", fmt.Errorf("failed to write command to switch: %w", err)
		}
		time.Sleep(arubaSwitchCommandPaceMs * time.Millisecond)
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
			return "", fmt.Errorf("failed to run command sequence: %w\noutput:\n%s", err, outputBuffer.String())
		}
	case <-time.After(sw.configTimeoutDuration):
		return "", fmt.Errorf("timed out waiting for command sequence to complete\ncommands:\n%s\npartial output:\n%s",
			formatArubaCommandBlock(commands), outputBuffer.String())
	}

	return outputBuffer.String(), nil
}

func arubaTeamSummary(teams [6]*model.Team) string {
	stations := []string{"R1", "R2", "R3", "B1", "B2", "B3"}
	entries := make([]string, len(stations))
	for i, station := range stations {
		if teams[i] == nil {
			entries[i] = fmt.Sprintf("%s=none", station)
			continue
		}
		entries[i] = fmt.Sprintf("%s=%d", station, teams[i].Id)
	}
	return strings.Join(entries, ", ")
}

func formatArubaCommandBlock(commands []string) string {
	lines := make([]string, len(commands))
	for i, command := range commands {
		lines[i] = fmt.Sprintf("%02d: %s", i+1, command)
	}
	return strings.Join(lines, "\n")
}
