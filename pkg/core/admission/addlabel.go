/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package admission

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

type patchOperation struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

const (
	PodLabelMixedKey             string              = "hc/mixed-pod"
	PodAnnotationPriorityKey     string              = "hc/priority"
	ContainerResourceCpuKey      corev1.ResourceName = "cmos.mixed/cpu"
	ContainerResourceMemoryKey   corev1.ResourceName = "cmos.mixed/memory"
	ContainerResourcePodCountKey string              = "cmos.mixed/podcount"
	PodNodeSelectorKey           string              = "cmos/mixed-schedule"
	// PodNodeSelectorLable string = `[
	//      { "op": "add", "path": "/spec/template/spec/nodeSelector", "value": {"cmos/mixed-schedule": "true"}}
	//  ]`
)

// func addLabel(ar admissionv1.AdmissionReview) *admissionv1.AdmissionResponse {
// 	klog.V(2).Info("calling add-label")
// 	obj := struct {
// 		metav1.ObjectMeta
// 		Data map[string]string
// 	}{}
// 	raw := ar.Request.Object.Raw
// 	err := json.Unmarshal(raw, &obj)
// 	if err != nil {
// 		klog.Error(err)
// 		return toAdmissionResponse(err)
// 	}

// 	reviewResponse := admissionv1.AdmissionResponse{}
// 	reviewResponse.Allowed = true
// 	if len(obj.ObjectMeta.Labels) == 0 {
// 		reviewResponse.Patch = []byte(addFirstLabelPatch)
// 	} else {
// 		reviewResponse.Patch = []byte(addAdditionalLabelPatch)
// 	}
// 	pt := admissionv1.PatchTypeJSONPatch
// 	reviewResponse.PatchType = &pt
// 	return &reviewResponse
// }

func mutatePodAnnotations(target map[string]string, added map[string]string) (patch []patchOperation) {
	for key, value := range added {
		if target == nil || target[key] == "" {
			target = map[string]string{}
			patch = append(patch, patchOperation{
				Op:   "add",
				Path: "/spec/template/metadata/annotations",
				Value: map[string]string{
					key: value,
				},
			})
		} else {
			patch = append(patch, patchOperation{
				Op:    "replace",
				Path:  "/spec/template/metadata/annotations/" + key,
				Value: value,
			})
		}
	}
	return
}

func mutatePodLables(target map[string]string, added map[string]string) (patch []patchOperation) {
	for key, value := range added {
		if target == nil || target[key] == "" {
			target = map[string]string{}
			patch = append(patch, patchOperation{
				Op:   "add",
				Path: "/spec/selector/matchLabels",
				Value: map[string]string{
					key: value,
				},
			})
			patch = append(patch, patchOperation{
				Op:   "add",
				Path: "/spec/template/metadata/labels",
				Value: map[string]string{
					key: value,
				},
			})
		} else {
			patch = append(patch, patchOperation{
				Op:    "replace",
				Path:  "/spec/selector/matchLabels/" + key,
				Value: value,
			})
			patch = append(patch, patchOperation{
				Op:    "replace",
				Path:  "/spec/template/metadata/labels/" + key,
				Value: value,
			})
		}
	}
	return
}

func mutateContainerResource(deployment *appsv1.Deployment) (patch []patchOperation) {
	containers := deployment.Spec.Template.Spec.Containers
	for index, container := range containers {
		reqs := container.Resources.Requests
		lims := container.Resources.Limits

		patch = append(patch, patchOperation{
			Op:   "add",
			Path: fmt.Sprintf("/spec/template/spec/containers/%d/resources/requests", index),
			Value: map[corev1.ResourceName]resource.Quantity{
				ContainerResourceCpuKey: reqs[corev1.ResourceCPU],
			},
		})
		patch = append(patch, patchOperation{
			Op:   "add",
			Path: fmt.Sprintf("/spec/template/spec/containers/%d/resources/requests", index),
			Value: map[corev1.ResourceName]resource.Quantity{
				ContainerResourceMemoryKey: reqs[corev1.ResourceMemory],
			},
		})
		patch = append(patch, patchOperation{
			Op:   "add",
			Path: fmt.Sprintf("/spec/template/spec/containers/%d/resources/limits", index),
			Value: map[corev1.ResourceName]resource.Quantity{
				ContainerResourceCpuKey: lims[corev1.ResourceCPU],
			},
		})
		patch = append(patch, patchOperation{
			Op:   "add",
			Path: fmt.Sprintf("/spec/template/spec/containers/%d/resources/limits", index),
			Value: map[corev1.ResourceName]resource.Quantity{
				ContainerResourceCpuKey: lims[corev1.ResourceMemory],
			},
		})

	}
	return
}
