package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

func loadDotEnv(path string) error {
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()

	lineNumber := 0
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		name, value, ok := strings.Cut(line, "=")
		name = strings.TrimSpace(name)
		if !ok || name == "" {
			return fmt.Errorf("invalid .env entry on line %d", lineNumber)
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && ((value[0] == '\'' && value[len(value)-1] == '\'') ||
			(value[0] == '"' && value[len(value)-1] == '"')) {
			if value[0] == '"' {
				value, err = strconv.Unquote(value)
				if err != nil {
					return fmt.Errorf("invalid quoted .env value on line %d: %w", lineNumber, err)
				}
			} else {
				value = value[1 : len(value)-1]
			}
		}
		if _, exists := os.LookupEnv(name); !exists {
			if err := os.Setenv(name, value); err != nil {
				return fmt.Errorf("setting .env variable %q: %w", name, err)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("reading .env: %w", err)
	}
	return nil
}
