package main

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	initRootCmd()
	os.Exit(m.Run())
}
