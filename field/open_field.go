// Open Field mode: lets teams drive outside of the match framework, with each alliance station controlling its own
// robot's enabled and autonomous/teleop state while the arena otherwise continues to act as an FMS.

package field

import "errors"

var errOpenFieldNotImplemented = errors.New("Open Field mode is not implemented yet.")

// Turns Open Field mode on or off. Can only be enabled while no match is in progress.
func (arena *Arena) SetOpenFieldMode(enabled bool) error {
	return errOpenFieldNotImplemented
}

// Enables or disables the robot in the given alliance station while in Open Field mode.
func (arena *Arena) OpenFieldSetEnabled(station string, enabled bool) error {
	return errOpenFieldNotImplemented
}

// Sets whether the robot in the given alliance station runs in autonomous (true) or teleop (false) mode.
func (arena *Arena) OpenFieldSetAuto(station string, auto bool) error {
	return errOpenFieldNotImplemented
}

// Runs a timed autonomous period for the given alliance station, after which the robot is disabled.
func (arena *Arena) OpenFieldRunAuto(station string) error {
	return errOpenFieldNotImplemented
}

// Immediately disables every robot on the field.
func (arena *Arena) OpenFieldDisableAll() {
}
