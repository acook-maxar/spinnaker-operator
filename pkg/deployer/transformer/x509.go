package transformer

import (
	spinnakerv1alpha1 "github.com/armory/spinnaker-operator/pkg/apis/spinnaker/v1alpha1"
	"github.com/armory/spinnaker-operator/pkg/generated"
	"github.com/armory/spinnaker-operator/pkg/halconfig"
	"github.com/armory/spinnaker-operator/pkg/util"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strconv"
)

type x509Transformer struct {
	*DefaultTransformer
	exposeLbTr *exposeLbTransformer
	svc        spinnakerv1alpha1.SpinnakerServiceInterface
	hc         *halconfig.SpinnakerConfig
	client     client.Client
	log        logr.Logger
}

type x509TransformerGenerator struct{}

// Transformer is in charge of excluding namespace manifests
func (g *x509TransformerGenerator) NewTransformer(svc spinnakerv1alpha1.SpinnakerServiceInterface,
	hc *halconfig.SpinnakerConfig, client client.Client, log logr.Logger) (Transformer, error) {
	base := &DefaultTransformer{}
	exGen := exposeLbTransformerGenerator{}
	exTr, err := exGen.NewTransformer(svc, hc, client, log)
	if err != nil {
		return nil, err
	}
	exLbTr := exTr.(*exposeLbTransformer)
	tr := x509Transformer{svc: svc, hc: hc, log: log, DefaultTransformer: base, exposeLbTr: exLbTr, client: client}
	base.ChildTransformer = &tr
	return &tr, nil
}

func (g *x509TransformerGenerator) GetName() string {
	return "X509"
}

func (t *x509Transformer) TransformManifests(scheme *runtime.Scheme, gen *generated.SpinnakerGeneratedConfig) error {
	exp := t.svc.GetExpose()
	if exp.Type == "" {
		return nil
	}

	gateConfig, ok := gen.Config["gate"]
	if !ok || gateConfig.Service == nil {
		return nil
	}
	// ignore error as api port property may not exist
	apiPort, err := t.hc.GetServiceConfigPropString("gate", "default.apiPort")
	if err != nil || apiPort == "" {
		return t.scheduleForRemovalIfNeeded(gateConfig, gen)
	}
	apiPortInt, err := strconv.ParseInt(apiPort, 10, 32)
	if err != nil {
		return err
	}
	x509Svc, err := t.createX509Service(int32(apiPortInt), gateConfig.Service)
	if err != nil {
		return err
	}
	t.exposeLbTr.applyExposeServiceConfig(x509Svc, "gate-x509")
	gen.Config["gate-x509"] = generated.ServiceConfig{
		Service: x509Svc,
	}
	return nil
}

func (t *x509Transformer) createX509Service(apiPort int32, gateSvc *corev1.Service) (*corev1.Service, error) {
	x509Svc := gateSvc.DeepCopy()
	x509Svc.Name = util.GateX509ServiceName
	if len(x509Svc.Spec.Ports) > 0 {
		x509Svc.Spec.Ports[0].Name = "gate-x509"
		x509Svc.Spec.Ports[0].Port = apiPort
		x509Svc.Spec.Ports[0].TargetPort = intstr.IntOrString{
			Type:   intstr.Int,
			IntVal: apiPort,
		}
	}
	return x509Svc, nil
}

func (t *x509Transformer) scheduleForRemovalIfNeeded(gateConfig generated.ServiceConfig, gen *generated.SpinnakerGeneratedConfig) error {
	x509Svc, err := util.GetService(util.GateX509ServiceName, gateConfig.Service.Namespace, t.client)
	if err != nil {
		return err
	}
	if x509Svc == nil {
		return nil
	}
	gen.Config["gate-x509"] = generated.ServiceConfig{
		ToDelete: []runtime.Object{x509Svc},
	}
	return nil
}
