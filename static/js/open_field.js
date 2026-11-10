// Client-side logic for the Open Field mode FTA page.

var websocket;
var lowBatteryThreshold = 8;

var showError = function(message) {
  $("#errorText").text(message);
  $("#errorMessage").show();
};

var send = function(type, data) {
  $("#errorMessage").hide();
  websocket.send(type, data);
};

// Sends a websocket message to turn Open Field mode on or off.
var setOpenFieldMode = function(enabled) {
  send("setOpenFieldMode", enabled);
};

// Sends a websocket message to immediately disable every robot.
var disableAll = function() {
  send("disableAll");
};

var disableStation = function(station) {
  send("disableStation", station);
};

// Sends a websocket message to assign the entered team to the given station.
var assignTeam = function(station) {
  var teamId = parseInt($("#station" + station + " .team-input").val());
  if (!(teamId > 0)) {
    clearStation(station);
    return;
  }
  send("assignTeam", { teamId: teamId, station: station });
};

var clearStation = function(station) {
  send("clearStation", station);
};

// Handles a websocket message to update the station and robot status.
var handleArenaStatus = function(data) {
  $("#openFieldModeStatus").text(data.OpenFieldMode ? "ON" : "OFF")
      .toggleClass("label-success", data.OpenFieldMode).toggleClass("label-default", !data.OpenFieldMode);

  $.each(data.AllianceStations, function(station, stationStatus) {
    var row = $("#station" + station);
    var wifiStatus = data.TeamWifiStatuses[station];
    var dsConn = stationStatus.DsConn;
    row.find(".radio-status").text(wifiStatus && wifiStatus.TeamId ? wifiStatus.TeamId : "");
    if (dsConn) {
      row.find(".ds-status").attr("data-status-ok", dsConn.DsLinked);
      var radioOkay = stationStatus.Team && wifiStatus && stationStatus.Team.Id === wifiStatus.TeamId &&
          wifiStatus.RadioLinked;
      row.find(".radio-status").attr("data-status-ok", radioOkay ? true : false);
      row.find(".robot-status").attr("data-status-ok", dsConn.RobotLinked);
      if (dsConn.RobotLinked) {
        row.find(".battery").text(dsConn.BatteryVoltage.toFixed(1) + "V")
            .css("color", dsConn.BatteryVoltage > lowBatteryThreshold ? "" : "#c33");
      } else {
        row.find(".battery").text("");
      }
    } else {
      row.find(".ds-status").attr("data-status-ok", "");
      row.find(".radio-status").attr("data-status-ok", "");
      row.find(".robot-status").attr("data-status-ok", "");
      row.find(".battery").text("");
    }
    row.find(".estop").text(stationStatus.Estop ? "E-STOP" : (stationStatus.Astop ? "A-STOP" : ""));

    var state = "";
    if (stationStatus.Team) {
      state = (stationStatus.OpenFieldEnabled ? "Enabled" : "Disabled") + " / " +
          (stationStatus.OpenFieldAuto ? "Auto" : "Teleop");
    }
    row.find(".robot-state").text(state).css("font-weight", stationStatus.OpenFieldEnabled ? "bold" : "");
  });
};

// Handles a websocket message to update the teams assigned to the stations.
var handleMatchLoad = function(data) {
  $.each(data.Teams, function(station, team) {
    var input = $("#station" + station + " .team-input");
    if (!input.is(":focus")) {
      input.val(team ? team.Id : "");
    }
  });
};

$(function() {
  // Set up the websocket back to the server.
  websocket = new CheesyWebsocket("/open_field/websocket", {
    arenaStatus: function(event) { handleArenaStatus(event.data); },
    matchLoad: function(event) { handleMatchLoad(event.data); },
    error: function(event) { showError(event.data); }
  });
});
