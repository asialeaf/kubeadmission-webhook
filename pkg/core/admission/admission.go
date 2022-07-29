package admission

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"

	admissionv1 "k8s.io/api/admission/v1"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog"

	"git.harmonycloud.cn/yeyazhou/kubeadmission-webhook/pkg/chilog"
	"git.harmonycloud.cn/yeyazhou/kubeadmission-webhook/pkg/config"
)

type API struct {
	// Protect against config, template and http client
	mtx sync.RWMutex

	conf   *config.Config
	logger log.Logger
}

func NewAPI(logger log.Logger) *API {
	return &API{
		logger: logger,
	}
}

func (api *API) Update(conf *config.Config) {
	api.mtx.Lock()
	defer api.mtx.Unlock()

	api.conf = conf
}

func (api *API) Routes() chi.Router {
	router := chi.NewRouter()
	router.Use(middleware.RealIP)
	router.Use(middleware.RequestLogger(&chilog.KitLogger{Logger: api.logger}))
	router.Use(middleware.Recoverer)
	router.HandleFunc("/mutate", api.serveMutate)
	// router.HandleFunc("/testconfig", api.getLimitList())
	return router
}

func (api *API) getRequiredList() (requirements []string) {
	logger := log.With(api.logger, "admission", "getlimitlist")

	api.mtx.RLock()
	conf := api.conf
	api.mtx.RUnlock()

	for _, v := range conf.Mixedreslist {
		requirements = append(requirements, v.Name+"/"+v.Namespace)
	}
	level.Info(logger).Log("msg", fmt.Sprintf("requiredList: %s", requirements))
	return requirements
}

// toAdmissionResponse is a helper function to create an AdmissionResponse
// with an embedded error
func toAdmissionResponse(err error) *admissionv1.AdmissionResponse {
	return &admissionv1.AdmissionResponse{
		Result: &metav1.Status{
			Message: err.Error(),
		},
	}
}

// admitFunc is the type we use for all of our validators and mutators
type admitFunc func(admissionv1.AdmissionReview) *admissionv1.AdmissionResponse

// serve handles the http portion of a request prior to handing to an admit
// function
func (api *API) serve(w http.ResponseWriter, r *http.Request, admit admitFunc) {
	var body []byte
	logger := log.With(api.logger, "admission", "server")
	if r.Body != nil {
		if data, err := ioutil.ReadAll(r.Body); err == nil {
			body = data
		}
	}

	// verify the content type is accurate
	contentType := r.Header.Get("Content-Type")
	if contentType != "application/json" {
		// klog.Errorf("contentType=%s, expect application/json", contentType)
		level.Error(logger).Log("msg", "contentType=%s, expect application/json", contentType)
		return
	}

	level.Info(logger).Log("msg", fmt.Sprintf("handling request: %s", body))
	// klog.V(2).Info(fmt.Sprintf("handling request: %s", body))

	// The AdmissionReview that was sent to the webhook
	requestedAdmissionReview := admissionv1.AdmissionReview{}

	// The AdmissionReview that will be returned
	responseAdmissionReview := admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			Kind:       "AdmissionReview",
			APIVersion: "admission.k8s.io/v1",
		},
	}

	deserializer := codecs.UniversalDeserializer()
	if _, _, err := deserializer.Decode(body, nil, &requestedAdmissionReview); err != nil {
		// klog.Error(err)
		level.Error(logger).Log("err", err)
		responseAdmissionReview.Response = toAdmissionResponse(err)
	} else {
		responseAdmissionReview.Response = admit(requestedAdmissionReview)
	}

	// Return the same UID
	responseAdmissionReview.Response.UID = requestedAdmissionReview.Request.UID

	// klog.V(2).Info(fmt.Sprintf("sending response: %v", responseAdmissionReview.Response))
	level.Info(logger).Log("msg", fmt.Sprintf("sending response: %v", responseAdmissionReview.Response))

	respBytes, err := json.Marshal(responseAdmissionReview)
	if err != nil {
		// klog.Error(err)
		level.Error(logger).Log("err", err)
	}
	if _, err := w.Write(respBytes); err != nil {
		// klog.Error(err)
		level.Error(logger).Log("err", err)
	}
}

func (api *API) serveMutate(w http.ResponseWriter, r *http.Request) {
	api.serve(w, r, api.mutate)
}

func (api *API) mutate(ar admissionv1.AdmissionReview) *admissionv1.AdmissionResponse {
	logger := log.With(api.logger, "admission", "mutate")
	req := ar.Request
	var deployment appsv1.Deployment
	var objectMeta *metav1.ObjectMeta
	level.Info(logger).Log("msg", fmt.Sprintf("AdmissionReview for Kind=%s, Namespace=%s Name=%s UID=%s", req.Kind.Kind, req.Namespace, req.Name, req.UID))

	switch req.Kind.Kind {
	case "Deployment":
		if err := json.Unmarshal(req.Object.Raw, &deployment); err != nil {
			level.Error(logger).Log("msg", "can't not unmarshal raw object", "err", err)
			return &admissionv1.AdmissionResponse{
				Result: &metav1.Status{
					Code:    http.StatusBadRequest,
					Message: err.Error(),
				},
			}

		}
		objectMeta = &deployment.ObjectMeta
	default:
		return &admissionv1.AdmissionResponse{
			Result: &metav1.Status{
				Code:    http.StatusBadRequest,
				Message: fmt.Sprintf("can't handle the kind(%s) object", req.Kind.Kind),
			},
		}
	}
	index, required := api.mutationRequired(objectMeta)
	if !required {
		return &admissionv1.AdmissionResponse{
			Allowed: true,
		}
	}
	mixed := api.conf.Mixedreslist[index].Mixed

	// 执行操作
	podAnnotations := map[string]string{
		PodAnnotationPriorityKey: strconv.FormatInt(api.conf.Mixedreslist[index].Priority, 10),
	}
	podLabels := map[string]string{
		PodLabelMixedKey: strconv.FormatBool(mixed),
	}
	nodeSelectolLabels := map[string]string{
		PodNodeSelectorKey: "true",
	}
	var patch []patchOperation
	patch = append(patch, mutatePodAnnotations(deployment.Spec.Template.ObjectMeta.Annotations, podAnnotations)...)
	patch = append(patch, mutatePodLables(deployment.Spec.Template.ObjectMeta.Labels, podLabels)...)

	// pod := deployment.Spec.Template.Spec
	if mixed {
		patch = append(patch, mutateNodeSelectol(deployment.Spec.Template.Spec.NodeSelector, nodeSelectolLabels)...)
		patch = append(patch, mutateContainerResource(&deployment)...)

	}
	level.Info(logger).Log("msg", fmt.Sprintf("Patch=%s", patch))
	patchBytes, err := json.Marshal(patch)
	if err != nil {
		klog.Errorf("patch marshal error: %v", err)
		return &admissionv1.AdmissionResponse{
			Result: &metav1.Status{
				Code:    http.StatusBadRequest,
				Message: err.Error(),
			},
		}
	}
	return &admissionv1.AdmissionResponse{
		Allowed: true,
		Patch:   patchBytes,
		PatchType: func() *admissionv1.PatchType {
			pt := admissionv1.PatchTypeJSONPatch
			return &pt
		}(),
	}

	// return addLabel(ar)
}
