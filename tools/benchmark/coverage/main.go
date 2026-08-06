package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
)

type commandManifest struct {
	Schema   string `json:"schema"`
	Commands []struct {
		Path string `json:"path"`
	} `json:"commands"`
}

type fixtureManifest struct {
	Fixtures []struct {
		Name string   `json:"name"`
		Kind string   `json:"kind"`
		Args []string `json:"args"`
	} `json:"fixtures"`
}

type coverageReport struct {
	Schema   string              `json:"schema"`
	Total    int                 `json:"total"`
	Covered  int                 `json:"covered"`
	Missing  []string            `json:"missing,omitempty"`
	Fixtures map[string][]string `json:"fixtures"`
}

func main() {
	commandsPath := flag.String("commands", "", "generated command manifest")
	fixturesPath := flag.String("fixtures", "fixtures/golden/manifest.json", "fixture manifest")
	flag.Parse()
	if *commandsPath == "" {
		fmt.Fprintln(os.Stderr, "--commands is required")
		os.Exit(2)
	}
	var commands commandManifest
	readJSON(*commandsPath, &commands)
	var fixtures fixtureManifest
	readJSON(*fixturesPath, &fixtures)
	report := coverageReport{Schema: "wowdata.command-coverage.v1", Total: len(commands.Commands), Fixtures: make(map[string][]string)}
	for _, command := range commands.Commands {
		words := strings.Fields(command.Path)
		for _, fixture := range fixtures.Fixtures {
			if containsWords(fixture.Args, words) {
				report.Fixtures[command.Path] = append(report.Fixtures[command.Path], fixture.Name)
			}
		}
		if len(report.Fixtures[command.Path]) == 0 {
			report.Missing = append(report.Missing, command.Path)
		} else {
			report.Covered++
			sort.Strings(report.Fixtures[command.Path])
		}
	}
	sort.Strings(report.Missing)
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		panic(err)
	}
	if len(report.Missing) > 0 {
		os.Exit(1)
	}
}

func readJSON(path string, output any) {
	data, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}
	if err := json.Unmarshal(data, output); err != nil {
		panic(err)
	}
}

func containsWords(args, words []string) bool {
	if len(words) == 0 || len(words) > len(args) {
		return false
	}
	for start := 0; start+len(words) <= len(args); start++ {
		matched := true
		for index := range words {
			if args[start+index] != words[index] {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}
