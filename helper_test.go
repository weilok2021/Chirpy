package main

import (
	"testing"
)

func TestReplaceProfane(t *testing.T) {
	expected := "This is a **** opinion I need to share with the world"
	actual := replaceProfane("This is a kerfuffle opinion I need to share with the world")
	if expected != actual {
		t.Errorf("expected: %s\n, actual: %s", expected, actual)
	}
}
