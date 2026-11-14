package model_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/winebarrel/hazel/model"
)

func TestKey(t *testing.T) {
	assert.Equal(t, "users", model.Key("UsErS"))
	// Only ASCII is folded, and other bytes are kept as they are.
	assert.Equal(t, "\u00c4b", model.Key("\u00c4B"))
	assert.Equal(t, "\xffa", model.Key("\xffA"))
}

func TestNewSchema(t *testing.T) {
	s := model.NewSchema()
	assert.Equal(t, 0, s.Tables.Len())
	assert.Equal(t, 0, s.Indexes.Len())
	assert.Equal(t, 0, s.Views.Len())
	assert.Equal(t, 0, s.Triggers.Len())
}
