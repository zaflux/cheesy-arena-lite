// Client-side logic for the Open Field mode station control page.

var websocket;
var lowBatteryThreshold = 8;
var openFieldMode = false;
var currentTeamId = 0;

// Shows an error message on the page without blocking (unlike alert(), which would swallow the disable hotkey).
var showError = function(message) {
  $("#errorText").text(message);
  $("#errorMessage").show();
};

var clearError = function() {
  $("#errorMessage").hide();
};

var send = function(type, data) {
  clearError();
  websocket.send(type, data);
};

// Sends a websocket message to claim this station for the entered team.
var claimStation = function() {
  var teamId = parseInt($("#teamNumber").val());
  if (!(teamId > 0)) {
    showError("Please enter a valid team number.");
    return;
  }
  if (currentTeamId !== 0 && currentTeamId !== teamId &&
      !confirm("Team " + currentTeamId + " is in this station. Replace it with Team " + teamId + "?")) {
    return;
  }
  send("claimStation", { teamId: teamId });
  $("#teamNumber").val("");
};

// Sends a websocket message to remove the team from this station.
var releaseStation = function() {
  if (currentTeamId !== 0 && !confirm("Release Team " + currentTeamId + " from this station?")) {
    return;
  }
  send("releaseStation");
};

var disableRobot = function() {
  send("disable");
};

var enableRobot = function() {
  send("enable");
};

var setAuto = function(auto) {
  send("setAuto", auto);
};

var runAuto = function() {
  send("runAuto");
};

var estopRobot = function() {
  send("estop");
};

// Updates the displayed team in this station.
var updateTeam = function(team) {
  currentTeamId = team ? team.Id : 0;
  $("#currentTeam").html(currentTeamId ? currentTeamId : "&ndash;");
  if (currentTeamId) {
    $("#wifiLink").attr("href", "/team_wifi?team=" + currentTeamId);
    $("#wifiLinkWrapper").show();
  } else {
    $("#wifiLinkWrapper").hide();
  }
};

// Handles a websocket message to update the station and robot status.
var handleArenaStatus = function(data) {
  var stationStatus = data.AllianceStations[station];
  if (!stationStatus) {
    return;
  }
  openFieldMode = data.OpenFieldMode;
  $("#openFieldOff").toggle(!openFieldMode);
  updateTeam(stationStatus.Team);

  // Connection status boxes, formatted the same way as on the match play page.
  var wifiStatus = data.TeamWifiStatuses[station];
  var dsConn = stationStatus.DsConn;
  var stopped = stationStatus.Estop || stationStatus.Astop;
  if (dsConn) {
    $(".ds-status").attr("data-status-ok", dsConn.DsLinked);
    var radioOkay = stationStatus.Team && wifiStatus && stationStatus.Team.Id === wifiStatus.TeamId &&
        wifiStatus.RadioLinked;
    $(".radio-status").attr("data-status-ok", radioOkay ? true : false);
    $(".robot-status").attr("data-status-ok", dsConn.RobotLinked);
    $(".robot-status").text(dsConn.RobotLinked ? "Linked" : "");
    if (dsConn.RobotLinked) {
      $("#battery").text(dsConn.BatteryVoltage.toFixed(1) + "V")
          .css("color", dsConn.BatteryVoltage > lowBatteryThreshold ? "" : "#c33");
    } else {
      $("#battery").html("&ndash;").css("color", "");
    }
    $("#fmsCommand").text((dsConn.Enabled ? "Enabled" : "Disabled") + " / " + (dsConn.Auto ? "Auto" : "Teleop"));
  } else {
    $(".ds-status").attr("data-status-ok", stationStatus.Team ? false : "");
    $(".radio-status").attr("data-status-ok", "");
    $(".robot-status").attr("data-status-ok", "").text("");
    $("#battery").html("&ndash;").css("color", "");
    $("#fmsCommand").html("&ndash;");
  }
  if (wifiStatus && wifiStatus.TeamId) {
    $(".radio-status").text(wifiStatus.TeamId);
  } else {
    $(".radio-status").text("");
  }
  $("#estop").text(stationStatus.Estop ? "E-STOPPED" : (stationStatus.Astop ? "A-STOPPED" : "OK"))
      .css("color", stopped ? "#c33" : "");

  // Overall robot state as requested from this page.
  var robotState = $("#robotState");
  if (stopped) {
    robotState.attr("data-state", "estop").text(stationStatus.Estop ? "E-STOPPED" : "A-STOPPED");
  } else if (stationStatus.OpenFieldEnabled) {
    var autoRemaining = stationStatus.OpenFieldAutoRemainingSec > 0 ?
        " (" + Math.ceil(stationStatus.OpenFieldAutoRemainingSec) + "s)" : "";
    robotState.attr("data-state", "enabled")
        .text("ENABLED - " + (stationStatus.OpenFieldAuto ? "AUTO" + autoRemaining : "TELEOP"));
  } else {
    robotState.attr("data-state", "disabled").text("DISABLED - " + (stationStatus.OpenFieldAuto ? "AUTO" : "TELEOP"));
  }
  $("#modeAuto").toggleClass("btn-primary", stationStatus.OpenFieldAuto)
      .toggleClass("btn-default", !stationStatus.OpenFieldAuto);
  $("#modeTeleop").toggleClass("btn-primary", !stationStatus.OpenFieldAuto)
      .toggleClass("btn-default", stationStatus.OpenFieldAuto);

  // Disable is always available; everything else requires Open Field mode and a team in the station.
  var canControl = openFieldMode && stationStatus.Team != null;
  $("#enableButton, #runAutoButton, #modeAuto, #modeTeleop").prop("disabled", !canControl || stopped);
  $("#releaseButton").prop("disabled", !canControl);
};

// Handles a websocket message to update the teams assigned to the stations.
var handleMatchLoad = function(data) {
  if (data.Teams) {
    updateTeam(data.Teams[station]);
  }
};

$(function() {
  if (!station) {
    return;
  }

  // Disable the robot whenever the space bar is pressed, like the real Driver Station.
  $(document).on("keydown", function(event) {
    if (event.which === 32 || event.key === " ") {
      event.preventDefault();
      disableRobot();
    }
  });

  // Set up the websocket back to the server.
  websocket = new CheesyWebsocket("/station_control/websocket", {
    arenaStatus: function(event) { handleArenaStatus(event.data); },
    matchLoad: function(event) { handleMatchLoad(event.data); },
    error: function(event) { showError(event.data); }
  });
});
