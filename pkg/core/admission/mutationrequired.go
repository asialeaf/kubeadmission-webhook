package admission

import (
	"fmt"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (api *API) mutationRequired(metadata *metav1.ObjectMeta) bool {
	var required bool
	logger := log.With(api.logger, "admission", "mutationRequired")
	level.Info(logger).Log("msg", "determine if a resource is in mixed list")

	names, namespaces := api.getLimitList()

	objName := metadata.Name
	objNamespace := metadata.Namespace

	level.Debug(logger).Log("msg", fmt.Sprintf("objName: %v,objNamespace:%v", objName, objNamespace))
	nameisExist := false
	namespaceisExist := false
	for _, v := range names {
		if objName == v {
			nameisExist = true
		}
	}
	for _, v := range namespaces {
		if objNamespace == v {
			namespaceisExist = true
		}
	}
	level.Debug(logger).Log("msg", fmt.Sprintf("nameisExist: %v,namespaceisExist:%v", nameisExist, namespaceisExist))
	required = nameisExist && namespaceisExist
	level.Info(logger).Log("msg", fmt.Sprintf("mutation policy for %s/%s: required: %v", metadata.Name, metadata.Namespace, required))
	return required
}
