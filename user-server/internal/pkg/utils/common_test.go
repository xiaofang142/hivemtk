package utils

import (
	"testing"
)

func TestBoolPtr(t *testing.T) {
	tests := []struct {
		input    bool
		expected bool
	}{
		{true, true},
		{false, false},
	}

	for _, tt := range tests {
		result := BoolPtr(tt.input)
		if result == nil {
			t.Fatalf("BoolPtr(%v) returned nil", tt.input)
		}
		if *result != tt.expected {
			t.Errorf("BoolPtr(%v) = %v, expected %v", tt.input, *result, tt.expected)
		}
	}
}

func TestGetBoolValue(t *testing.T) {
	trueVal := true
	falseVal := false

	tests := []struct {
		name       string
		input      *bool
		defaultVal bool
		expected   bool
	}{
		{"nil pointer with true default", nil, true, true},
		{"nil pointer with false default", nil, false, false},
		{"true value", &trueVal, false, true},
		{"false value", &falseVal, true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetBoolValue(tt.input, tt.defaultVal)
			if result != tt.expected {
				t.Errorf("GetBoolValue(%v, %v) = %v, expected %v", tt.input, tt.defaultVal, result, tt.expected)
			}
		})
	}
}

func TestContainsKeyword(t *testing.T) {
	tests := []struct {
		name     string
		message  string
		keywords string
		expected bool
	}{
		{"single keyword match", "Hello world", "Hello", true},
		{"multiple keywords match", "Hello world", "Hi,Hello,Greetings", true},
		{"no keyword match", "Hello world", "Goodbye,Bye", false},
		{"empty keywords", "Hello world", "", true},
		{"empty message", "", "Hello", false},
		{"case sensitive", "Hello World", "hello", false},
		{"partial match", "Say Hello to everyone", "Hello", true},
		{"keyword with spaces in list", "Hello world", "Hi, Hello ,Greetings", true},
		{"multiple keywords with spaces", "Hello world", "Hi,Hello,Greetings", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ContainsKeyword(tt.message, tt.keywords)
			if result != tt.expected {
				t.Errorf("ContainsKeyword(%q, %q) = %v, expected %v", tt.message, tt.keywords, result, tt.expected)
			}
		})
	}
}

func TestContainsKeyword_CommaSeparated(t *testing.T) {
	message := "I want to buy a car"
	keywords := "bike,train,car,bus"

	result := ContainsKeyword(message, keywords)
	if !result {
		t.Errorf("Expected ContainsKeyword to return true for keyword 'car'")
	}
}

func TestContainsKeyword_NoMatch(t *testing.T) {
	message := "I like apples"
	keywords := "orange,banana,grape"

	result := ContainsKeyword(message, keywords)
	if result {
		t.Errorf("Expected ContainsKeyword to return false")
	}
}
