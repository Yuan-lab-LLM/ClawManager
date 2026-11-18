// Test bridge only. Production execution goes through the scoped runtime sandbox.
package main

import (
	"context"
	"encoding/json"
	"github.com/iamlovingit/clawmanager-agent/workspacegit"
	"os"
)

func main() {
	var request struct {
		Root    string               `json:"root"`
		Request workspacegit.Request `json:"request"`
	}
	if json.NewDecoder(os.Stdin).Decode(&request) != nil {
		os.Exit(2)
	}
	service, err := workspacegit.New(request.Root)
	var result workspacegit.Response
	if err == nil {
		result, err = service.Execute(context.Background(), request.Request)
	}
	if err != nil {
		json.NewEncoder(os.Stdout).Encode(map[string]any{"error": err.Error()})
		os.Exit(1)
	}
	json.NewEncoder(os.Stdout).Encode(result)
}
