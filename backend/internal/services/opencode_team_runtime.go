package services

import (
	"encoding/json"
	"strings"

	"clawreef/internal/models"
)

const openCodeTeamProtocol = "opencode-team-v1"

func filterOpenCodeTeamPods(instance *models.Instance, pods []models.RuntimePod) []models.RuntimePod {
	if instance == nil || (instance.Type != "opencode" && instance.Type != "deepseek-harness") || instance.EnvironmentOverridesJSON == nil {
		return pods
	}
	var env map[string]string
	if json.Unmarshal([]byte(*instance.EnvironmentOverridesJSON), &env) != nil {
		return nil
	}
	if !strings.EqualFold(env["CLAWMANAGER_TEAM_ENABLED"], "true") {
		return pods
	}
	wanted := openCodeTeamProtocol
	if instance.Type == "deepseek-harness" {
		wanted = "dsh-team-v1"
	}
	result := make([]models.RuntimePod, 0, len(pods))
	for _, pod := range pods {
		if stringValue(pod.TeamPluginVersion) == wanted {
			result = append(result, pod)
		}
	}
	return result
}
