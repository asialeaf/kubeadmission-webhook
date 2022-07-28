package admission

import (
	"fmt"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (api *API) mutationRequired(metadata *metav1.ObjectMeta) (index int, required bool) {
	required = false
	logger := log.With(api.logger, "admission", "mutationRequired")
	level.Info(logger).Log("msg", "determine if a resource is in mixed list")
	requirements := api.getRequiredList()
	res := metadata.Name + "/" + metadata.Namespace

	for i, v := range requirements {
		if res == v {
			required = true
			index = i
		}
	}
	level.Info(logger).Log("msg", fmt.Sprintf("mutation policy for %s/%s: required: %v", metadata.Name, metadata.Namespace, required))
	return index, required
}
