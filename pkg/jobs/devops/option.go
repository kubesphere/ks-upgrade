package devops

import (
	appv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
)

// helm values of devops

type Config struct {
	Global    *GlobalOptions    `json:"global,omitempty"    yaml:"global,omitempty"`
	Extension *ExtensionOptions `json:"extension,omitempty" yaml:"extension,omitempty"`
	Agent     *AgentOptions     `json:"agent,omitempty"     yaml:"agent,omitempty"`
}

type Override struct {
	Agent *AgentOptions `json:"agent,omitempty" yaml:"agent,omitempty"`
}

type GlobalOptions struct {
	Image *Image `json:"image,omitempty" yaml:"image,omitempty"`
}

type Image struct {
	Registry    string              `json:"registry,omitempty"    yaml:"registry,omitempty"`
	PullSecrets []map[string]string `json:"pullSecrets,omitempty" yaml:"pullSecrets,omitempty"`
}

type ExtensionOptions struct {
	Apiserver *Workload `json:"apiserver,omitempty" yaml:"apiserver,omitempty"`
}

type Workload struct {
	Replicas  *int32                `json:"replicas,omitempty"  yaml:"replicas,omitempty"`
	Image     *Image                `json:"image,omitempty"     yaml:"image,omitempty"`
	Resources *ResourceRequirements `json:"resources,omitempty" yaml:"resources,omitempty"`
}

func (w *Workload) FromDeployment(deployment appv1.Deployment) {
	w.Replicas = deployment.Spec.Replicas
	w.Resources = new(ResourceRequirements)
	w.Resources.Parse(deployment.Spec.Template.Spec.Containers[0].Resources)
}

type ResourceRequirements struct {
	Requests *Resource `json:"requests,omitempty" yaml:"requests,omitempty"`
	Limits   *Resource `json:"limits,omitempty"   yaml:"limits,omitempty"`
}

func (r *ResourceRequirements) Parse(v1rr v1.ResourceRequirements) {
	if v1rr.Requests != nil {
		r.Requests = new(Resource)
		r.Requests.Cpu = v1rr.Requests.Cpu().String()
		r.Requests.Memory = v1rr.Requests.Memory().String()
	}
	if v1rr.Limits != nil {
		r.Limits = new(Resource)
		r.Limits.Cpu = v1rr.Limits.Cpu().String()
		r.Limits.Memory = v1rr.Limits.Memory().String()
	}
}

type Resource struct {
	Cpu    string `json:"cpu,omitempty"    yaml:"cpu,omitempty"`
	Memory string `json:"memory,omitempty" yaml:"memory,omitempty"`
}

type AgentOptions struct {
	Apiserver  *Workload `json:"apiserver,omitempty"  yaml:"apiserver,omitempty"`
	Controller *Workload `json:"controller,omitempty" yaml:"controller,omitempty"`

	Jenkins *JenkinsOptions `json:"jenkins,omitempty" yaml:"jenkins,omitempty"`
	Argocd  *ArgoOptions    `json:"argocd,omitempty"  yaml:"argocd,omitempty"`
}

func (o *AgentOptions) SetApiserver(deployment appv1.Deployment) {
	if o.Apiserver == nil {
		o.Apiserver = new(Workload)
	}
	o.Apiserver.FromDeployment(deployment)
}

func (o *AgentOptions) SetController(deployment appv1.Deployment) {
	if o.Controller == nil {
		o.Controller = new(Workload)
	}
	o.Controller.FromDeployment(deployment)
}

type JenkinsOptions struct {
	Master        *JenkinsMaster `json:"Master,omitempty"        yaml:"Master,omitempty"`
	Persistence   *Persistence   `json:"persistence,omitempty"   yaml:"persistence,omitempty"`
	SecurityRealm *SecurityRealm `json:"securityRealm,omitempty" yaml:"securityRealm,omitempty"`
}

type JenkinsMaster struct {
	*Workload
	ServiceType   string     `json:"ServiceType,omitempty" yaml:"ServiceType,omitempty"`
	Smtp          *Smtp      `json:"smtp,omitempty"        yaml:"smtp,omitempty"`
	Sonarqube     *Sonarqube `json:"sonarqube,omitempty"   yaml:"sonarqube,omitempty"`
	AdminPassword string     `json:"AdminPassword,omitempty" yaml:"AdminPassword,omitempty"`
}

type Smtp struct {
	Host     string `json:"EMAIL_SMTP_HOST,omitempty" yaml:"EMAIL_SMTP_HOST,omitempty"`
	Port     string `json:"EMAIL_SMTP_PORT,omitempty" yaml:"EMAIL_SMTP_PORT,omitempty"`
	UseSSL   bool   `json:"EMAIL_USE_SSL,omitempty"   yaml:"EMAIL_USE_SSL,omitempty"`
	FromName string `json:"EMAIL_FROM_NAME,omitempty" yaml:"EMAIL_FROM_NAME,omitempty"`
	FromAddr string `json:"EMAIL_FROM_ADDR,omitempty" yaml:"EMAIL_FROM_ADDR,omitempty"`
	FromPass string `json:"EMAIL_FROM_PASS,omitempty" yaml:"EMAIL_FROM_PASS,omitempty"`
}

type Sonarqube struct {
	ServerUrl string `json:"serverUrl,omitempty" yaml:"serverUrl,omitempty"`
	AuthToken string `json:"authToken,omitempty" yaml:"authToken,omitempty"`
}

type Persistence struct {
	ExistingClaim string `json:"existingClaim,omitempty"         yaml:"existingClaim,omitempty"`
}

type SecurityRealm struct {
	Oidc *OpenIdConnect `json:"openIdConnect,omitempty" yaml:"openIdConnect,omitempty"`
}

type OpenIdConnect struct {
	KsCoreApi string `json:"kubesphereCoreApi,omitempty" yaml:"kubesphereCoreApi,omitempty"`
}

type ArgoOptions struct {
	Dex            *Workload `json:"dex,omitempty"            yaml:"dex,omitempty"`
	Controller     *Workload `json:"controller,omitempty"     yaml:"controller,omitempty"`
	ApplicationSet *Workload `json:"applicationSet,omitempty" yaml:"applicationSet,omitempty"`
	Redis          *Workload `json:"redis,omitempty"          yaml:"redis,omitempty"`
	Notifications  *Workload `json:"notifications,omitempty"  yaml:"notifications,omitempty"`
	RepoServer     *Workload `json:"repoServer,omitempty"     yaml:"repoServer,omitempty"`
	Server         *Workload `json:"server,omitempty"         yaml:"server,omitempty"`
}

func (o *ArgoOptions) cleanImageNil() {
	if o.Dex.Image != nil && o.Dex.Image.Registry == "" && len(o.Dex.Image.PullSecrets) == 0 {
		o.Dex.Image = nil
	}
	if o.ApplicationSet.Image != nil && o.ApplicationSet.Image.Registry == "" && len(o.ApplicationSet.Image.PullSecrets) == 0 {
		o.ApplicationSet.Image = nil
	}
	if o.Redis.Image != nil && o.Redis.Image.Registry == "" && len(o.Redis.Image.PullSecrets) == 0 {
		o.Redis.Image = nil
	}
}
