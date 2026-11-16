package k8s

import (
	"reflect"

	corev1 "k8s.io/api/core/v1"
)

// Images and their advertised identities are mutable configuration. All other
// startup, security and storage fields retain the original sleep contract.
func runtimeSleepTemplateMatches(current corev1.PodTemplateSpec, expected *RuntimePodScaleSnapshot) bool {
	if expected.WakeConfigurationFrozen {
		return reflect.DeepEqual(current, expected.Template)
	}
	a, b := current.DeepCopy(), expected.Template.DeepCopy()
	for _, template := range []*corev1.PodTemplateSpec{a, b} {
		for _, containers := range [][]corev1.Container{template.Spec.Containers, template.Spec.InitContainers} {
			for i := range containers {
				containers[i].Image = ""
				for e := range containers[i].Env {
					switch containers[i].Env[e].Name {
					case "CLAWMANAGER_RUNTIME_IMAGE_REF", "CLAWMANAGER_RUNTIME_IMAGE_DIGEST":
						containers[i].Env[e].Value = ""
					}
				}
			}
		}
	}
	return reflect.DeepEqual(a, b)
}

func runtimeSleepGenerationMatches(actual int64, expected *RuntimePodScaleSnapshot, delta int64) bool {
	if expected.WakeConfigurationFrozen {
		return actual == expected.DeploymentGeneration+delta
	}
	// A Deployment generation changes for normal image revisions as well as
	// replica writes. Ownership comes from the sleep marker and native UID/CAS.
	return actual >= expected.DeploymentGeneration+delta
}
