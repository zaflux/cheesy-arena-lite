package network

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTeamSubnetOctets(t *testing.T) {
	second, third := teamSubnetOctets(254)
	assert.Equal(t, 2, second)
	assert.Equal(t, 54, third)

	second, third = teamSubnetOctets(10173)
	assert.Equal(t, 101, second)
	assert.Equal(t, 73, third)
}

func TestTeamSubnetPrefix(t *testing.T) {
	assert.Equal(t, "10.2.54", teamSubnetPrefix(254))
	assert.Equal(t, "10.101.73", teamSubnetPrefix(10173))
}

func TestTeamIDFromSubnetOctets(t *testing.T) {
	assert.Equal(t, 254, teamIDFromSubnetOctets(2, 54))
	assert.Equal(t, 10173, teamIDFromSubnetOctets(101, 73))
}
