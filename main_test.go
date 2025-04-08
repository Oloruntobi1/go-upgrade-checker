package main

import (
	"testing"
)

func TestExtractExportedFunctionSignature(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Valid exported function",
			input:    "// SomeFunc does something\nfunc SomeFunc(a string) error",
			expected: "func SomeFunc(a string) error",
		},
		{
			name:     "Non-exported function",
			input:    "// someFunc does something\nfunc someFunc(a string) error",
			expected: "",
		},
		{
			name:     "Invalid input - no newline",
			input:    "func SomeFunc(a string) error",
			expected: "",
		},
		{
			name:     "Invalid input - no func keyword",
			input:    "// SomeFunc does something\nSomeFunc(a string) error",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractExportedFunctionSignature(tt.input)
			if result != tt.expected {
				t.Errorf("extractExportedFunctionSignature() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestFindChangedFunctions(t *testing.T) {
	usedFunctions := map[string]struct{}{
		"func ChangingFunc(a string) error": {},
		"func UnchangedFunc()":              {},
		"func RemovedFunc()":                {},
	}
	newFunctions := map[string]struct{}{
		"func ChangingFunc(a string, c int) error": {},
		"func UnchangedFunc()":                     {},
		"func NewFunc()":                           {},
	}

	changed := findChangedFunctions(usedFunctions, newFunctions)

	// Check for changed function
	if newSig, exists := changed["func ChangingFunc(a string) error"]; !exists {
		t.Error("Expected to find changed function ChangingFunc")
	} else if newSig != "func ChangingFunc(a string, c int) error" {
		t.Errorf("Expected new signature 'func ChangingFunc(a string, c int) error', got %s", newSig)
	}

	// Check for unchanged function
	if _, exists := changed["func UnchangedFunc()"]; exists {
		t.Error("UnchangedFunc should not be marked as changed")
	}

	// Check for removed function
	if newSig, exists := changed["func RemovedFunc()"]; !exists {
		t.Error("Expected to find removed function RemovedFunc")
	} else if newSig != "removed" {
		t.Errorf("Expected removed function to be marked as 'removed', got %s", newSig)
	}
}
