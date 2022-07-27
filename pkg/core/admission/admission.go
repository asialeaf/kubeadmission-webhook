package admission

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"

	"k8s.io/api/admission/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog"

	"git.harmonycloud.cn/yeyazhou/kubeadmission-webhook/pkg/chilog"
	"git.harmonycloud.cn/yeyazhou/kubeadmission-webhook/pkg/config"
	"git.harmonycloud.cn/yeyazhou/kubeadmission-webhook/pkg/utils"
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
	router.HandleFunc("/add-label", api.serveAddLabel)
	// router.HandleFunc("/testconfig", api.testConfig)
	return router
}

func (api *API) getLimitList() (names, namespaces []string) {
	logger := log.With(api.logger, "admission", "getlimitlist")

	api.mtx.RLock()
	conf := api.conf
	api.mtx.RUnlock()

	for _, v := range conf.Mixedreslist {
		names = append(names, v.Name)
		namespaces = append(namespaces, v.Namespace)
	}
	level.Info(logger).Log("names", fmt.Sprintf("%s", names))
	level.Info(logger).Log("namespaces", fmt.Sprintf("%s", namespaces))
	return names, namespaces
}

// toAdmissionResponse is a helper function to create an AdmissionResponse
// with an embedded error
func toAdmissionResponse(err error) *v1beta1.AdmissionResponse {
	return &v1beta1.AdmissionResponse{
		Result: &metav1.Status{
			Message: err.Error(),
		},
	}
}

// admitFunc is the type we use for all of our validators and mutators
type admitFunc func(v1beta1.AdmissionReview) *v1beta1.AdmissionResponse

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
	requestedAdmissionReview := v1beta1.AdmissionReview{}

	// The AdmissionReview that will be returned
	responseAdmissionReview := v1beta1.AdmissionReview{}

	deserializer := codecs.UniversalDeserializer()
	if _, _, err := deserializer.Decode(body, nil, &requestedAdmissionReview); err != nil {
		// klog.Error(err)
		level.Error(logger).Log("err", err)
		responseAdmissionReview.Response = toAdmissionResponse(err)
	} else if api.isMixedList(requestedAdmissionReview) {
		// pass to admitFunc
		responseAdmissionReview.Response = admit(requestedAdmissionReview)
	} else {
		responseAdmissionReview.Response.Allowed = true
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

func (api *API) serveAddLabel(w http.ResponseWriter, r *http.Request) {
	api.serve(w, r, addLabel)
}

func (api *API) isMixedList(ar v1beta1.AdmissionReview) bool {
	logger := log.With(api.logger, "admission", "ismixedlist")
	level.Info(logger).Log("msg", "determine if a resource is in mixed list")

	names, namespaces := api.getLimitList()

	obj := struct {
		metav1.ObjectMeta
		Data map[string]string
	}{}
	raw := ar.Request.Object.Raw
	err := json.Unmarshal(raw, &obj)
	if err != nil {
		klog.Error(err)
	}
	objName := obj.ObjectMeta.Name
	objNamespace := obj.ObjectMeta.Namespace
	if utils.InArray(objName, names) && utils.InArray(objNamespace, namespaces) {
		return true
	} else {
		return false
	}
}
