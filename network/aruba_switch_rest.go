// Copyright 2026 Team 1732. All Rights Reserved.
//
// Methods for configuring an HP/Aruba 2920-series switch using the ArubaOS-S REST API.

package network

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Team254/cheesy-arena-lite/model"
)

const (
	arubaSwitchRestTimeoutSec = 10
	arubaCliBatchPollInterval = 250 * time.Millisecond
	arubaCliBatchPollTimeout  = 15 * time.Second

	// ArubaOS-S REST v1 uses these ACL and DHCP configuration constants.
	arubaRestAclType        = "AT_EXTENDED_IPV4"
	arubaRestAclDirection   = "AD_VLAN_INBOUND"
	arubaRestDhcpLeaseDays  = 7
	arubaRestDhcpRangeStart = 5
	arubaRestDhcpRangeEnd   = 20
)

var arubaSwitchRestApiVersions = []string{
	"v1",
}

type ArubaSwitchRest struct {
	address  string
	username string
	password string

	mutex      sync.Mutex
	httpClient *http.Client
}

func NewArubaSwitchRest(address, username, password string) *ArubaSwitchRest {
	return &ArubaSwitchRest{
		address:  strings.TrimSpace(address),
		username: username,
		password: password,
		httpClient: &http.Client{
			Timeout: arubaSwitchRestTimeoutSec * time.Second,
		},
	}
}

func (sw *ArubaSwitchRest) diagLogf(format string, args ...any) {
	if !isSwitchDiagnosticLoggingEnabled() {
		return
	}
	log.Printf(format, args...)
}

// ArubaSwitchRestBatch is a variant of ArubaSwitchRest that sends configuration
// as a single base64-encoded batch via POST /rest/v7/cli_batch instead of
// posting individual CLI lines.
type ArubaSwitchRestBatch struct {
	ArubaSwitchRest
}

func NewArubaSwitchRestBatch(address, username, password string) *ArubaSwitchRestBatch {
	return &ArubaSwitchRestBatch{
		ArubaSwitchRest: *NewArubaSwitchRest(address, username, password),
	}
}

// ConfigureTeamEthernet overrides the per-line CLI path and uses cli_batch instead.
func (sw *ArubaSwitchRestBatch) ConfigureTeamEthernet(teams [6]*model.Team) error {
	sw.mutex.Lock()
	defer sw.mutex.Unlock()

	if sw.address == "" {
		return fmt.Errorf("aruba REST switch address is empty")
	}

	sw.diagLogf("ArubaSwitchRESTBatch(%s): requested Ethernet reconfiguration for teams [%s]",
		sw.address, arubaTeamSummary(teams))

	session, err := sw.loginSession()
	if err != nil {
		return err
	}
	defer func() {
		if logoutErr := sw.logoutSession(session); logoutErr != nil {
			sw.diagLogf("ArubaSwitchRESTBatch(%s): logout failed: %s", sw.address, logoutErr.Error())
		}
	}()

	removeCommands, addCommands, hasTeams := sw.buildCliConfigureCommands(teams)
	removeBatchCommands := prepareCliBatchCommands(removeCommands)
	addBatchCommands := prepareCliBatchCommands(addCommands)
	sw.diagLogf("ArubaSwitchRESTBatch(%s): reset command block:\n%s", sw.address, formatArubaCommandBlock(removeCommands))
	if hasTeams {
		sw.diagLogf("ArubaSwitchRESTBatch(%s): apply command block:\n%s", sw.address, formatArubaCommandBlock(addCommands))
	} else {
		sw.diagLogf("ArubaSwitchRESTBatch(%s): no teams assigned; only reset/remove commands will be sent.", sw.address)
	}

	if err := sw.runCliBatchCommandSequence(session, removeBatchCommands); err != nil {
		return fmt.Errorf("run reset batch: %w", err)
	}

	if hasTeams {
		if err := sw.runCliBatchCommandSequence(session, addBatchCommands); err != nil {
			return fmt.Errorf("run apply batch: %w", err)
		}
	}

	sw.diagLogf("ArubaSwitchRESTBatch(%s): completed Ethernet reconfiguration for teams [%s]",
		sw.address, arubaTeamSummary(teams))
	return nil
}

// Sets up wired networks for the given set of teams using v1 login + v7 CLI commands.
func (sw *ArubaSwitchRest) ConfigureTeamEthernet(teams [6]*model.Team) error {
	sw.mutex.Lock()
	defer sw.mutex.Unlock()

	if sw.address == "" {
		return fmt.Errorf("aruba REST switch address is empty")
	}

	sw.diagLogf("ArubaSwitchREST(%s): requested Ethernet reconfiguration for teams [%s]",
		sw.address, arubaTeamSummary(teams))

	session, err := sw.loginSession()
	if err != nil {
		return err
	}
	defer func() {
		if logoutErr := sw.logoutSession(session); logoutErr != nil {
			sw.diagLogf("ArubaSwitchREST(%s): logout failed: %s", sw.address, logoutErr.Error())
		}
	}()

	removeCommands, addCommands, hasTeams := sw.buildCliConfigureCommands(teams)
	sw.diagLogf("ArubaSwitchREST(%s): reset command block:\n%s", sw.address, formatArubaCommandBlock(removeCommands))
	if hasTeams {
		sw.diagLogf("ArubaSwitchREST(%s): apply command block:\n%s", sw.address, formatArubaCommandBlock(addCommands))
	} else {
		sw.diagLogf("ArubaSwitchREST(%s): no teams assigned; only reset/remove commands will be sent.", sw.address)
	}

	if err := sw.runCliCommandSequence(session, removeCommands); err != nil {
		return fmt.Errorf("run reset command block: %w", err)
	}

	if hasTeams {
		if err := sw.runCliCommandSequence(session, addCommands); err != nil {
			return fmt.Errorf("run apply command block: %w", err)
		}
	}

	sw.diagLogf("ArubaSwitchREST(%s): completed Ethernet reconfiguration for teams [%s]",
		sw.address, arubaTeamSummary(teams))
	return nil
}

func (sw *ArubaSwitchRest) buildCliConfigureCommands(teams [6]*model.Team) ([]string, []string, bool) {
	removeCommands := []string{"configure", "no dhcp-server enable"}
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
	removeCommands = append(removeCommands, "write memory", "exit")

	addCommands := []string{"configure"}
	hasTeams := false

	addTeamVlan := func(team *model.Team, vlan int) {
		if team == nil {
			return
		}
		hasTeams = true
		teamSubnet := teamSubnetPrefix(team.Id)
		gatewayIP := fmt.Sprintf("%s.%d", teamSubnet, arubaSwitchTeamGateway)
		networkIP := fmt.Sprintf("%s.0", teamSubnet)
		aclName := 100 + vlan

		addCommands = append(addCommands,
			fmt.Sprintf("vlan %d", vlan),
			fmt.Sprintf("ip address %s 255.255.255.0", gatewayIP),
			"dhcp-server",
			fmt.Sprintf("ip access-group \"%d\" vlan-in", aclName),
			"exit",
			fmt.Sprintf("ip access-list extended \"%d\"", aclName),
			fmt.Sprintf("10 permit ip %s 0.0.0.255 host %s", networkIP, ServerIpAddress),
			"20 permit udp any eq 68 any eq 67",
			"30 deny ip any any",
			"exit",
			fmt.Sprintf("dhcp-server pool \"dhcp%d\"", vlan),
			"authoritative",
			fmt.Sprintf("network %s 255.255.255.0", networkIP),
			fmt.Sprintf("range %s.5 %s.20", teamSubnet, teamSubnet),
			fmt.Sprintf("default-router %s", gatewayIP),
			"dns-server 8.8.8.8",
			"dns-server 8.8.4.4",
			"lease 00:07:00",
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
		addCommands = append(addCommands, "dhcp-server enable", "write memory", "exit")
	}

	return removeCommands, addCommands, hasTeams
}

func (sw *ArubaSwitchRest) runCliCommandSequence(session *arubaRestSession, commands []string) error {
	for _, command := range commands {
		if strings.TrimSpace(command) == "" {
			continue
		}
		if _, err := sw.restPostCli(session, command); err != nil {
			return fmt.Errorf("run CLI command %q: %w", command, err)
		}
	}
	return nil
}

func (sw *ArubaSwitchRest) restPostCli(session *arubaRestSession, command string) (string, error) {
	cliBaseURL, err := sw.restBaseURL("v7")
	if err != nil {
		return "", err
	}
	uri := cliBaseURL + "/cli"

	body := map[string]string{"cmd": command}
	encoded, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshal CLI body for %q: %w", command, err)
	}

	req, err := http.NewRequest(http.MethodPost, uri, bytes.NewReader(encoded))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	applyArubaRestAuth(req, session)

	resp, responseBody, err := sw.doRequest(req)
	if err != nil {
		sw.logRestCall(http.MethodPost, uri, string(encoded), 0, "", err)
		return "", err
	}

	sw.logRestCall(http.MethodPost, uri, string(encoded), resp.StatusCode, responseBody, nil)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return responseBody, fmt.Errorf("POST %s returned status %d: %s", uri, resp.StatusCode, compactForLog(responseBody))
	}

	return responseBody, nil
}

// runCliBatchCommandSequence sends all commands as a single POST to /rest/v7/cli_batch.
// The firmware accepts a base64-encoded block of newline-separated CLI commands.
// On success it returns the CliBatchCommandResult; individual command results are
// available via GET /rest/v7/cli_batch/status (CliBatchResult) which this function
// also fetches and logs.
func (sw *ArubaSwitchRest) runCliBatchCommandSequence(session *arubaRestSession, commands []string) error {
	if len(commands) == 0 {
		return nil
	}

	filtered := make([]string, 0, len(commands))
	for _, c := range commands {
		if strings.TrimSpace(c) != "" {
			filtered = append(filtered, c)
		}
	}

	raw := strings.Join(filtered, "\n")
	encoded := base64.StdEncoding.EncodeToString([]byte(raw))

	cliBaseURL, err := sw.restBaseURL("v7")
	if err != nil {
		return err
	}
	uri := cliBaseURL + "/cli_batch"

	body := map[string]string{"cli_batch_base64_encoded": encoded}
	marshaled, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal cli_batch body: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, uri, bytes.NewReader(marshaled))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	applyArubaRestAuth(req, session)

	// Log the decoded commands, not the base64 blob, for readability.
	sw.logRestCall(http.MethodPost, uri, raw, 0, "", nil)

	resp, responseBody, err := sw.doRequest(req)
	if err != nil {
		sw.logRestCall(http.MethodPost, uri, raw, 0, "", err)
		return err
	}
	sw.diagLogf("ArubaSwitchREST(%s): cli_batch POST status=%d response=%s", sw.address, resp.StatusCode, compactForLog(responseBody))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("cli_batch POST returned status %d: %s", resp.StatusCode, compactForLog(responseBody))
	}

	result, err := sw.waitForCliBatchResult(session)
	if err != nil {
		return err
	}

	failures := make([]string, 0)
	for _, entry := range result.ExecLogs {
		sw.diagLogf("ArubaSwitchREST(%s): cli_batch cmd=%q status=%s result=%s",
			sw.address, entry.Cmd, entry.Status, strings.TrimSpace(entry.Result))
		if entry.Status == "CCS_FAILURE" {
			if isIgnorableCliBatchFailure(entry.Cmd, entry.Result) {
				sw.diagLogf("ArubaSwitchREST(%s): cli_batch ignoring benign failure for cmd=%q result=%s",
					sw.address, entry.Cmd, strings.TrimSpace(entry.Result))
				continue
			}
			failures = append(failures, fmt.Sprintf("%q: %s", entry.Cmd, strings.TrimSpace(entry.Result)))
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("cli_batch: %d command(s) failed: %s", len(failures), strings.Join(failures, "; "))
	}
	return nil
}

func prepareCliBatchCommands(commands []string) []string {
	filtered := make([]string, 0, len(commands))
	for _, command := range commands {
		trimmed := strings.TrimSpace(command)
		if trimmed == "" || trimmed == "write memory" {
			continue
		}
		filtered = append(filtered, command)
	}

	if len(filtered) > 0 && strings.TrimSpace(filtered[len(filtered)-1]) == "exit" {
		filtered = filtered[:len(filtered)-1]
	}

	return filtered
}

func isIgnorableCliBatchFailure(command, result string) bool {
	command = strings.TrimSpace(command)
	result = strings.ToLower(strings.TrimSpace(result))

	switch {
	case strings.HasPrefix(command, "no dhcp-server pool ") && strings.Contains(result, "does not exist"):
		return true
	case strings.HasPrefix(command, "no ip access-group ") && strings.Contains(result, "has not been configured"):
		return true
	case strings.HasPrefix(command, "no ip access-list extended ") && strings.Contains(result, "has not been configured"):
		return true
	default:
		return false
	}
}

func (sw *ArubaSwitchRest) waitForCliBatchResult(session *arubaRestSession) (*arubaCliBatchResult, error) {
	cliBaseURL, err := sw.restBaseURL("v7")
	if err != nil {
		return nil, err
	}
	resultPath := cliBaseURL + "/cli_batch/status"

	deadline := time.Now().Add(arubaCliBatchPollTimeout)
	for {
		resultBody, err := sw.restGetAbsolute(session, resultPath)
		if err != nil {
			return nil, fmt.Errorf("fetch cli_batch result: %w", err)
		}

		var result arubaCliBatchResult
		if err := json.Unmarshal([]byte(resultBody), &result); err != nil {
			return nil, fmt.Errorf("parse cli_batch result: %w", err)
		}

		sw.diagLogf("ArubaSwitchREST(%s): cli_batch last_status=%s", sw.address, result.LastStatus)
		switch result.LastStatus {
		case "CLS_COMPLETED":
			return &result, nil
		case "CLS_IN_PROGRESS":
			if time.Now().After(deadline) {
				return nil, fmt.Errorf("cli_batch did not complete within %s", arubaCliBatchPollTimeout)
			}
			time.Sleep(arubaCliBatchPollInterval)
		default:
			return nil, fmt.Errorf("cli_batch returned unexpected last_status %q", result.LastStatus)
		}
	}
}

type arubaCliBatchResult struct {
	LastStatus string `json:"last_status"`
	ExecLogs   []struct {
		Cmd    string `json:"cmd"`
		Status string `json:"status"`
		Result string `json:"result"`
	} `json:"cmd_exec_logs"`
}

func (sw *ArubaSwitchRest) restGetAbsolute(session *arubaRestSession, requestURL string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, requestURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	applyArubaRestAuth(req, session)

	resp, responseBody, err := sw.doRequest(req)
	if err != nil {
		sw.logRestCall(http.MethodGet, requestURL, "", 0, "", err)
		return "", err
	}

	sw.logRestCall(http.MethodGet, requestURL, "", resp.StatusCode, responseBody, nil)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return responseBody, fmt.Errorf("GET %s returned status %d: %s", requestURL, resp.StatusCode, compactForLog(responseBody))
	}
	return responseBody, nil
}

func (sw *ArubaSwitchRest) applyTeamVLANConfiguration(session *arubaRestSession, vlan int, team *model.Team) error {
	aclName := fmt.Sprintf("%d", 100+vlan)
	aclID := fmt.Sprintf("%s~%s", aclName, arubaRestAclType)
	teamSubnet := teamSubnetPrefix(team.Id)
	gatewayIP := fmt.Sprintf("%s.%d", teamSubnet, arubaSwitchTeamGateway)
	networkIP := teamSubnet + ".0"
	poolName := fmt.Sprintf("dhcp%d", vlan)

	// Apply VLAN interface IP first so it is not blocked by later ACL operations.
	if ipErr := sw.upsertVlanIPAddress(session, vlan, gatewayIP); ipErr != nil {
		return fmt.Errorf("configure IP address: %w", ipErr)
	}

	// Enable DHCP server on VLAN.
	enableDhcp := map[string]any{
		"vlan_id":                vlan,
		"is_dhcp_server_enabled": true,
	}
	if _, putErr := sw.restPut(session, fmt.Sprintf("/vlans/%d", vlan), enableDhcp); putErr != nil {
		return fmt.Errorf("enable DHCP: %w", putErr)
	}

	// Create DHCP pool.
	dhcpBody := map[string]any{
		"pool_name": poolName,
		"network_ip": map[string]any{
			"version": "IAV_IP_V4",
			"octets":  networkIP,
		},
		"network_mask": map[string]any{
			"version": "IAV_IP_V4",
			"octets":  "255.255.255.0",
		},
		"default_routers": []map[string]any{
			{
				"version": "IAV_IP_V4",
				"octets":  gatewayIP,
			},
		},
		"ip_range": []map[string]any{
			{
				"ip_start": map[string]any{
					"version": "IAV_IP_V4",
					"octets":  fmt.Sprintf("%s.%d", teamSubnet, arubaRestDhcpRangeStart),
				},
				"ip_end": map[string]any{
					"version": "IAV_IP_V4",
					"octets":  fmt.Sprintf("%s.%d", teamSubnet, arubaRestDhcpRangeEnd),
				},
			},
		},
		"dns_servers":     []any{},
		"netbios_servers": []any{},
		"options":         []any{},
		"lease_time": map[string]any{
			"days":    arubaRestDhcpLeaseDays,
			"hours":   0,
			"minutes": 0,
		},
	}
	// Disable global DHCP server before creating pool (some firmware versions require this).
	if _, putErr := sw.restPut(session, "/dhcp-server", map[string]any{"is_dhcp_server_enabled": false}); putErr != nil {
		sw.diagLogf("ArubaSwitchREST(%s): disable global DHCP before pool create: %s (continuing)", sw.address, putErr)
	}
	_, poolPostErr := sw.restPost(session, "/dhcp-server/pools", dhcpBody)
	// Re-enable global DHCP server regardless of pool creation outcome.
	if _, putErr := sw.restPut(session, "/dhcp-server", map[string]any{"is_dhcp_server_enabled": true}); putErr != nil {
		sw.diagLogf("ArubaSwitchREST(%s): re-enable global DHCP after pool create: %s (continuing)", sw.address, putErr)
	}
	if poolPostErr != nil {
		return fmt.Errorf("create DHCP pool %s: %w", poolName, poolPostErr)
	}

	// Create ACL object.
	aclBody := map[string]any{
		"acl_name": aclName,
		"acl_type": arubaRestAclType,
	}
	if _, postErr := sw.restPost(session, "/acls", aclBody); postErr != nil {
		if !isAclAlreadyExistsError(postErr) {
			return fmt.Errorf("create ACL %s: %w", aclID, postErr)
		}

		existingAclID, findErr := sw.findAclIDByNameAndType(session, aclName, arubaRestAclType)
		if findErr != nil {
			return fmt.Errorf("find existing ACL %s after already-exists error: %w", aclID, findErr)
		}
		if existingAclID == "" {
			return fmt.Errorf("ACL name %s already exists but no %s ACL was found", aclName, arubaRestAclType)
		}
		aclID = existingAclID
		sw.diagLogf("ArubaSwitchREST(%s): reusing existing ACL %s after already-exists response", sw.address, aclID)
	}

	// ACL rule 10: permit ip {subnet}.0/0.0.0.255 -> host ServerIpAddress
	rule10 := map[string]any{
		"sequence_no": 10,
		"acl_id":      aclID,
		"acl_action":  "AA_PERMIT",
		"traffic_match": map[string]any{
			"protocol_type":          "PT_IP",
			"source_ip_address":      networkIP,
			"source_ip_mask":         "0.0.0.255",
			"destination_ip_address": ServerIpAddress,
			"destination_ip_mask":    "255.255.255.255",
		},
		"is_log": false,
	}
	if _, postErr := sw.restPost(session, fmt.Sprintf("/acls/%s/rules", aclID), rule10); postErr != nil {
		return fmt.Errorf("create ACL rule 10 on %s: %w", aclID, postErr)
	}

	// ACL rule 20: permit udp any eq 68 -> any eq 67 (DHCP)
	rule20 := map[string]any{
		"sequence_no": 20,
		"acl_id":      aclID,
		"acl_action":  "AA_PERMIT",
		"traffic_match": map[string]any{
			"protocol_type":          "PT_UDP",
			"source_ip_address":      "0.0.0.0",
			"source_ip_mask":         "255.255.255.255",
			"source_port":            68,
			"destination_ip_address": "0.0.0.0",
			"destination_ip_mask":    "255.255.255.255",
			"destination_port":       67,
		},
		"is_log": false,
	}
	if _, postErr := sw.restPost(session, fmt.Sprintf("/acls/%s/rules", aclID), rule20); postErr != nil {
		return fmt.Errorf("create ACL rule 20 on %s: %w", aclID, postErr)
	}

	// ACL rule 30: deny ip any -> any
	rule30 := map[string]any{
		"sequence_no": 30,
		"acl_id":      aclID,
		"acl_action":  "AA_DENY",
		"traffic_match": map[string]any{
			"protocol_type":          "PT_IP",
			"source_ip_address":      "0.0.0.0",
			"source_ip_mask":         "255.255.255.255",
			"destination_ip_address": "0.0.0.0",
			"destination_ip_mask":    "255.255.255.255",
		},
		"is_log": false,
	}
	if _, postErr := sw.restPost(session, fmt.Sprintf("/acls/%s/rules", aclID), rule30); postErr != nil {
		return fmt.Errorf("create ACL rule 30 on %s: %w", aclID, postErr)
	}

	// Bind ACL to VLAN (inbound).
	bindBody := map[string]any{
		"vlan_id":   vlan,
		"acl_id":    aclID,
		"direction": arubaRestAclDirection,
	}
	if _, postErr := sw.restPost(session, "/vlans-access-groups", bindBody); postErr != nil {
		return fmt.Errorf("bind ACL %s to VLAN %d: %w", aclID, vlan, postErr)
	}

	sw.diagLogf("ArubaSwitchREST(%s): configured VLAN %d for team %d (%s)", sw.address, vlan, team.Id, teamSubnet)
	return nil
}

func (sw *ArubaSwitchRest) listAclRuleSequenceNumbers(session *arubaRestSession, aclID string) ([]int, error) {
	path := fmt.Sprintf("/acls/%s/rules", aclID)
	body, status, err := sw.restGet(session, path)
	if err != nil {
		if isArubaDeleteMissingResource(status, err) {
			return nil, nil
		}
		return nil, err
	}

	type aclRuleItem struct {
		SequenceNo int `json:"sequence_no"`
	}
	var parsed struct {
		Elements []aclRuleItem `json:"acl_rule_element"`
	}
	if unmarshalErr := json.Unmarshal([]byte(body), &parsed); unmarshalErr != nil {
		return nil, fmt.Errorf("parse ACL rules response: %w", unmarshalErr)
	}

	seqs := make([]int, 0, len(parsed.Elements))
	for _, item := range parsed.Elements {
		if item.SequenceNo > 0 {
			seqs = append(seqs, item.SequenceNo)
		}
	}
	return seqs, nil
}

func (sw *ArubaSwitchRest) findAclIDsByName(session *arubaRestSession, aclName string) ([]string, error) {
	body, status, err := sw.restGet(session, "/acls")
	if err != nil {
		if isArubaDeleteMissingResource(status, err) {
			return nil, nil
		}
		return nil, err
	}

	type aclElement struct {
		AclID   string `json:"acl_id"`
		AclName string `json:"acl_name"`
		AclType string `json:"acl_type"`
	}
	var parsed struct {
		Elements []aclElement `json:"acl_element"`
	}
	if unmarshalErr := json.Unmarshal([]byte(body), &parsed); unmarshalErr != nil {
		return nil, fmt.Errorf("parse ACL collection response: %w", unmarshalErr)
	}

	ids := make([]string, 0)
	seen := make(map[string]struct{})
	for _, element := range parsed.Elements {
		if element.AclName != aclName {
			continue
		}

		id := strings.TrimSpace(element.AclID)
		if id == "" && element.AclType != "" {
			id = fmt.Sprintf("%s~%s", aclName, element.AclType)
		}
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}

	return ids, nil
}

func (sw *ArubaSwitchRest) findAclIDByNameAndType(session *arubaRestSession, aclName, aclType string) (string, error) {
	ids, err := sw.findAclIDsByName(session, aclName)
	if err != nil {
		return "", err
	}
	wantSuffix := "~" + aclType
	for _, id := range ids {
		if strings.HasSuffix(id, wantSuffix) {
			return id, nil
		}
	}
	return "", nil
}

func (sw *ArubaSwitchRest) listDhcpPoolNames(session *arubaRestSession) (map[string]struct{}, error) {
	body, _, err := sw.restGet(session, "/dhcp-server/pools")
	if err != nil {
		return nil, err
	}

	var parsed struct {
		Elements []struct {
			PoolName string `json:"pool_name"`
		} `json:"dhcp_server_pool_element"`
	}
	if unmarshalErr := json.Unmarshal([]byte(body), &parsed); unmarshalErr != nil {
		return nil, fmt.Errorf("parse DHCP pool collection response: %w", unmarshalErr)
	}

	pools := make(map[string]struct{}, len(parsed.Elements))
	for _, element := range parsed.Elements {
		name := strings.TrimSpace(element.PoolName)
		if name == "" {
			continue
		}
		pools[name] = struct{}{}
	}
	return pools, nil
}

func (sw *ArubaSwitchRest) listVlanAclBindingKeys(session *arubaRestSession) (map[string]struct{}, error) {
	body, _, err := sw.restGet(session, "/vlans-access-groups")
	if err != nil {
		return nil, err
	}

	var parsed struct {
		Elements []struct {
			VlanID    int    `json:"vlan_id"`
			AclID     string `json:"acl_id"`
			Direction string `json:"direction"`
		} `json:"acl_vlan_policy_element"`
	}
	if unmarshalErr := json.Unmarshal([]byte(body), &parsed); unmarshalErr != nil {
		return nil, fmt.Errorf("parse VLAN ACL binding collection response: %w", unmarshalErr)
	}

	bindings := make(map[string]struct{}, len(parsed.Elements))
	for _, element := range parsed.Elements {
		aclID := strings.TrimSpace(element.AclID)
		direction := strings.TrimSpace(element.Direction)
		if element.VlanID <= 0 || aclID == "" || direction == "" {
			continue
		}
		key := fmt.Sprintf("%d-%s-%s", element.VlanID, aclID, direction)
		bindings[key] = struct{}{}
	}

	return bindings, nil
}

func (sw *ArubaSwitchRest) vlanHasIPSubnets(session *arubaRestSession, vlanID int) (bool, error) {
	body, _, err := sw.restGet(session, fmt.Sprintf("/vlans/%d/ipaddresses", vlanID))
	if err != nil {
		return false, err
	}

	var parsed struct {
		Elements []any `json:"ip_address_subnet_element"`
	}
	if unmarshalErr := json.Unmarshal([]byte(body), &parsed); unmarshalErr != nil {
		return false, fmt.Errorf("parse VLAN %d IP subnet response: %w", vlanID, unmarshalErr)
	}

	return len(parsed.Elements) > 0, nil
}

func (sw *ArubaSwitchRest) upsertVlanIPAddress(session *arubaRestSession, vlanID int, gatewayIP string) error {
	path := fmt.Sprintf("/vlans/%d/ipaddresses", vlanID)
	body := map[string]any{
		"vlan_id":         vlanID,
		"ip_address_mode": "IAAM_STATIC",
		"ip_address": map[string]any{
			"version": "IAV_IP_V4",
			"octets":  gatewayIP,
		},
		"ip_mask": map[string]any{
			"version": "IAV_IP_V4",
			"octets":  "255.255.255.0",
		},
	}

	if _, err := sw.restPost(session, path, body); err != nil {
		lower := strings.ToLower(err.Error())
		if !strings.Contains(lower, "already exist") && !strings.Contains(lower, "status 409") {
			return err
		}

		// Replace any existing IP subnet on this VLAN and retry create.
		if code, delErr := sw.restDelete(session, path); delErr != nil && !isArubaDeleteMissingResource(code, delErr) {
			return fmt.Errorf("replace existing VLAN %d IP subnet: %w", vlanID, delErr)
		}
		if _, retryErr := sw.restPost(session, path, body); retryErr != nil {
			return retryErr
		}
	}

	appliedIP, err := sw.getVlanStaticIPAddress(session, vlanID)
	if err != nil {
		return err
	}
	if appliedIP != gatewayIP {
		return fmt.Errorf("VLAN %d IP verify failed: expected %s got %s", vlanID, gatewayIP, appliedIP)
	}

	return nil
}

func (sw *ArubaSwitchRest) getVlanStaticIPAddress(session *arubaRestSession, vlanID int) (string, error) {
	body, _, err := sw.restGet(session, fmt.Sprintf("/vlans/%d/ipaddresses", vlanID))
	if err != nil {
		return "", err
	}

	var parsed struct {
		Elements []struct {
			IPAddressMode string `json:"ip_address_mode"`
			IPAddress     struct {
				Octets string `json:"octets"`
			} `json:"ip_address"`
		} `json:"ip_address_subnet_element"`
	}
	if unmarshalErr := json.Unmarshal([]byte(body), &parsed); unmarshalErr != nil {
		return "", fmt.Errorf("parse VLAN %d IP address response: %w", vlanID, unmarshalErr)
	}

	for _, element := range parsed.Elements {
		if element.IPAddressMode == "IAAM_STATIC" {
			return strings.TrimSpace(element.IPAddress.Octets), nil
		}
	}

	return "", fmt.Errorf("VLAN %d has no static IP address entry", vlanID)
}

// restGet sends a GET request to the given REST path.
// Returns the response body and HTTP status code.
func (sw *ArubaSwitchRest) restGet(session *arubaRestSession, path string) (string, int, error) {
	req, err := http.NewRequest(http.MethodGet, session.baseURL+path, nil)
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Accept", "application/json")
	applyArubaRestAuth(req, session)

	resp, responseBody, err := sw.doRequest(req)
	if err != nil {
		sw.logRestCall(http.MethodGet, session.baseURL+path, "", 0, "", err)
		return "", 0, err
	}

	sw.logRestCall(http.MethodGet, session.baseURL+path, "", resp.StatusCode, responseBody, nil)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return responseBody, resp.StatusCode,
			fmt.Errorf("GET %s returned status %d: %s", path, resp.StatusCode, compactForLog(responseBody))
	}
	return responseBody, resp.StatusCode, nil
}

// restPost sends a POST request to the given REST path with a JSON body.
// Returns the response body and HTTP status code.
func (sw *ArubaSwitchRest) restPost(session *arubaRestSession, path string, body any) (string, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshal POST body for %s: %w", path, err)
	}
	req, err := http.NewRequest(http.MethodPost, session.baseURL+path, bytes.NewReader(encoded))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	applyArubaRestAuth(req, session)

	resp, responseBody, err := sw.doRequest(req)
	if err != nil {
		sw.logRestCall(http.MethodPost, session.baseURL+path, string(encoded), 0, "", err)
		return "", err
	}
	sw.logRestCall(http.MethodPost, session.baseURL+path, string(encoded), resp.StatusCode, responseBody, nil)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return responseBody, fmt.Errorf("POST %s returned status %d: %s", path, resp.StatusCode, compactForLog(responseBody))
	}
	return responseBody, nil
}

// restPut sends a PUT request to the given REST path with a JSON body.
func (sw *ArubaSwitchRest) restPut(session *arubaRestSession, path string, body any) (string, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshal PUT body for %s: %w", path, err)
	}
	req, err := http.NewRequest(http.MethodPut, session.baseURL+path, bytes.NewReader(encoded))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	applyArubaRestAuth(req, session)

	resp, responseBody, err := sw.doRequest(req)
	if err != nil {
		sw.logRestCall(http.MethodPut, session.baseURL+path, string(encoded), 0, "", err)
		return "", err
	}
	sw.logRestCall(http.MethodPut, session.baseURL+path, string(encoded), resp.StatusCode, responseBody, nil)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return responseBody, fmt.Errorf("PUT %s returned status %d: %s", path, resp.StatusCode, compactForLog(responseBody))
	}
	return responseBody, nil
}

// restDelete sends a DELETE request to the given REST path.
// Returns the HTTP status code along with any error.
func (sw *ArubaSwitchRest) restDelete(session *arubaRestSession, path string) (int, error) {
	req, err := http.NewRequest(http.MethodDelete, session.baseURL+path, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/json")
	applyArubaRestAuth(req, session)

	resp, responseBody, err := sw.doRequest(req)
	if err != nil {
		sw.logRestCall(http.MethodDelete, session.baseURL+path, "", 0, "", err)
		return 0, err
	}
	sw.logRestCall(http.MethodDelete, session.baseURL+path, "", resp.StatusCode, responseBody, nil)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp.StatusCode, fmt.Errorf("DELETE %s returned status %d: %s", path, resp.StatusCode, compactForLog(responseBody))
	}
	return resp.StatusCode, nil
}

type arubaRestSession struct {
	apiVersion string
	baseURL    string
	cookie     *http.Cookie
	cookieRaw  string
	authToken  string
}

func (sw *ArubaSwitchRest) loginSession() (*arubaRestSession, error) {
	for _, version := range arubaSwitchRestApiVersions {
		baseURL, err := sw.restBaseURL(version)
		if err != nil {
			return nil, err
		}
		loginURL := baseURL + "/login-sessions"
		sw.diagLogf("ArubaSwitchREST(%s): attempting REST login against %s", sw.address, loginURL)

		// JSON schema (request):
		// {
		//   "type": "object",
		//   "required": ["userName", "password"],
		//   "properties": {
		//     "userName": {"type": "string"},
		//     "password": {"type": "string"}
		//   }
		// }
		payload := map[string]string{
			"userName": sw.username,
			"password": sw.password,
		}
		body, _ := json.Marshal(payload)
		req, err := http.NewRequest(http.MethodPost, loginURL, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")

		resp, responseBody, err := sw.doRequest(req)
		if err != nil {
			sw.diagLogf("ArubaSwitchREST(%s): REST login request failed for %s: %s", sw.address, version, err.Error())
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			sw.diagLogf("ArubaSwitchREST(%s): REST login failed for %s status=%d body=%s", sw.address, version,
				resp.StatusCode, compactForLog(responseBody))
			continue
		}

		session := extractArubaRestSession(version, baseURL, resp, responseBody)
		if session == nil {
			sw.diagLogf("ArubaSwitchREST(%s): REST login succeeded for %s but no auth artifact found. body=%s",
				sw.address, version, compactForLog(responseBody))
			continue
		}

		sw.diagLogf("ArubaSwitchREST(%s): REST login succeeded with API version %s (cookie=%t token=%t).",
			sw.address, version, session.cookie != nil || session.cookieRaw != "", session.authToken != "")
		return session, nil
	}

	return nil, fmt.Errorf("ArubaSwitchREST(%s): unable to establish REST login session on known API versions", sw.address)
}

func (sw *ArubaSwitchRest) logoutSession(session *arubaRestSession) error {
	logoutURL := session.baseURL + "/login-sessions"
	req, err := http.NewRequest(http.MethodDelete, logoutURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	applyArubaRestAuth(req, session)

	resp, responseBody, err := sw.doRequest(req)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("logout failed with status %d body=%s", resp.StatusCode, compactForLog(responseBody))
	}
	return nil
}

func (sw *ArubaSwitchRest) restBaseURL(version string) (string, error) {
	address := sw.address
	if !strings.Contains(address, "://") {
		address = "http://" + address
	}
	parsed, err := url.Parse(address)
	if err != nil {
		return "", fmt.Errorf("invalid switch address '%s': %w", sw.address, err)
	}

	// ArubaOS-S REST on this deployment is HTTP-only and listens on TCP/80.
	hostname := parsed.Hostname()
	if hostname == "" {
		hostname = parsed.Host
	}
	parsed.Scheme = "http"
	parsed.Host = net.JoinHostPort(hostname, "80")
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/rest/" + version
	return strings.TrimRight(parsed.String(), "/"), nil
}

func (sw *ArubaSwitchRest) doRequest(req *http.Request) (*http.Response, string, error) {
	resp, err := sw.httpClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp, "", err
	}
	return resp, string(bodyBytes), nil
}

func firstSessionCookie(cookies []*http.Cookie) *http.Cookie {
	if len(cookies) == 0 {
		return nil
	}
	for _, cookie := range cookies {
		if strings.EqualFold(cookie.Name, "sessionId") || strings.EqualFold(cookie.Name, "session_id") {
			return cookie
		}
	}
	return cookies[0]
}

func applyArubaRestAuth(req *http.Request, session *arubaRestSession) {
	if session == nil {
		return
	}
	if session.cookie != nil {
		req.AddCookie(session.cookie)
	}
	if session.cookieRaw != "" {
		req.Header.Add("Cookie", session.cookieRaw)
	}
	if session.authToken != "" {
		req.Header.Set("X-Auth-Token", session.authToken)
	}
}

func extractArubaRestSession(version, baseURL string, resp *http.Response, responseBody string) *arubaRestSession {
	session := &arubaRestSession{apiVersion: version, baseURL: baseURL}

	if cookie := firstSessionCookie(resp.Cookies()); cookie != nil {
		session.cookie = cookie
	}

	if headerCookie := strings.TrimSpace(resp.Header.Get("Set-Cookie")); headerCookie != "" {
		parts := strings.Split(headerCookie, ";")
		if len(parts) > 0 {
			session.cookieRaw = strings.TrimSpace(parts[0])
		}
	}

	if token := strings.TrimSpace(resp.Header.Get("X-Auth-Token")); token != "" {
		session.authToken = token
	}

	// ArubaOS-S v1 may return auth material in the JSON body rather than Set-Cookie.
	var body map[string]any
	if err := json.Unmarshal([]byte(responseBody), &body); err == nil {
		if session.cookie == nil && session.cookieRaw == "" {
			for _, key := range []string{"cookie", "Cookie", "sessionCookie", "session_cookie"} {
				if raw, ok := body[key].(string); ok && strings.TrimSpace(raw) != "" {
					parts := strings.Split(strings.TrimSpace(raw), ";")
					session.cookieRaw = strings.TrimSpace(parts[0])
					break
				}
			}
		}
		if session.authToken == "" {
			for _, key := range []string{"token", "authToken", "auth_token", "sessionToken", "csrfToken", "csrf_token"} {
				if raw, ok := body[key].(string); ok && strings.TrimSpace(raw) != "" {
					session.authToken = strings.TrimSpace(raw)
					break
				}
			}
		}
	}

	if session.cookie == nil && session.cookieRaw == "" && session.authToken == "" {
		return nil
	}

	// If we only have a raw cookie string "name=value", expose as structured cookie too.
	if session.cookie == nil && session.cookieRaw != "" {
		parts := strings.SplitN(session.cookieRaw, "=", 2)
		if len(parts) == 2 {
			session.cookie = &http.Cookie{Name: strings.TrimSpace(parts[0]), Value: strings.TrimSpace(parts[1])}
		}
	}

	return session
}

func compactForLog(text string) string {
	return strings.TrimSpace(text)
}

func (sw *ArubaSwitchRest) logRestCall(method, uri, requestBody string, statusCode int, responseBody string, err error) {
	if !isSwitchDiagnosticLoggingEnabled() {
		return
	}

	if err != nil {
		log.Printf("ArubaSwitchREST(%s): REST %s %s failed: %s", sw.address, method, uri, err)
		return
	}

	if requestBody != "" {
		log.Printf("ArubaSwitchREST(%s): REST %s %s request=%s status=%d response=%s",
			sw.address, method, uri, compactForLog(requestBody), statusCode, compactForLog(responseBody))
		return
	}

	log.Printf("ArubaSwitchREST(%s): REST %s %s status=%d response=%s",
		sw.address, method, uri, statusCode, compactForLog(responseBody))
}

func isArubaDeleteMissingResource(statusCode int, err error) bool {
	if err == nil {
		return false
	}

	if statusCode == http.StatusNotFound {
		return true
	}

	if statusCode == http.StatusBadRequest {
		text := strings.ToLower(err.Error())
		return strings.Contains(text, "ip address not configured")
	}

	if statusCode != http.StatusInternalServerError {
		return false
	}

	// ArubaOS-S can return 500 for missing ACL/rule deletes instead of 404.
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "invalid input: access-list") || strings.Contains(text, "invalid input: ace")
}

func isAclAlreadyExistsError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "already exist") || strings.Contains(text, "already exists")
}
