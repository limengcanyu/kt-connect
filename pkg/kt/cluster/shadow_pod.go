package cluster

import (
	"context"
	"errors"
	"github.com/alibaba/kt-connect/pkg/common"
	"github.com/alibaba/kt-connect/pkg/kt/options"
	"github.com/alibaba/kt-connect/pkg/kt/util"
	"github.com/rs/zerolog/log"
	appV1 "k8s.io/api/apps/v1"
	coreV1 "k8s.io/api/core/v1"
)

// GetKtResources fetch all kt pods and deployments
func GetKtResources(ctx context.Context, k KubernetesInterface, namespace string) ([]coreV1.Pod, []appV1.Deployment, []coreV1.Service, error) {
	pods, err := k.GetPodsByLabel(ctx, map[string]string{common.ControlBy: common.KubernetesTool}, namespace)
	if err != nil {
		return nil, nil, nil, err
	}
	deployments, err := k.GetDeploymentsByLabel(ctx, map[string]string{common.ControlBy: common.KubernetesTool}, namespace)
	if err != nil {
		return nil, nil, nil, err
	}
	services, err := k.GetServicesByLabel(ctx, map[string]string{common.ControlBy: common.KubernetesTool}, namespace)
	if err != nil {
		return nil, nil, nil, err
	}
	return pods.Items, deployments.Items, services.Items, nil
}

// GetOrCreateShadow create shadow
func GetOrCreateShadow(ctx context.Context, k KubernetesInterface, name string, options *options.DaemonOptions, labels, annotations, envs map[string]string) (
	string, string, *util.SSHCredential, error) {

	privateKeyPath := util.PrivateKeyPath(name)

	// extra labels must be applied after origin labels
	for k, v := range util.String2Map(options.WithLabels) {
		labels[k] = v
	}
	for k, v := range util.String2Map(options.WithAnnotations) {
		annotations[k] = v
	}
	annotations[common.KtUser] = util.GetLocalUserName()
	resourceMeta := ResourceMeta{
		Name:        name,
		Namespace:   options.Namespace,
		Labels:      labels,
		Annotations: annotations,
	}
	sshKeyMeta := SSHkeyMeta{
		SshConfigMapName: name,
		PrivateKeyPath:   privateKeyPath,
	}

	if options.ConnectOptions != nil && options.ConnectOptions.ShareShadow {
		pod, generator, err2 := tryGetExistingShadowRelatedObjs(ctx, k, &resourceMeta, &sshKeyMeta)
		if err2 != nil {
			return "", "", nil, err2
		}
		if pod != nil && generator != nil {
			podIP, podName, credential := shadowResult(pod, generator)
			return podIP, podName, credential, nil
		}
	}

	podMeta := PodMetaAndSpec{
		Meta:  &resourceMeta,
		Image: options.Image,
		Envs:  envs,
	}
	return createShadow(ctx, k, &podMeta, &sshKeyMeta, options)
}

func createShadow(ctx context.Context, k KubernetesInterface, metaAndSpec *PodMetaAndSpec, sshKeyMeta *SSHkeyMeta, options *options.DaemonOptions) (
	podIP string, podName string, credential *util.SSHCredential, err error) {

	generator, err := util.Generate(sshKeyMeta.PrivateKeyPath)
	if err != nil {
		return
	}

	configMap, err := k.CreateConfigMapWithSshKey(ctx, metaAndSpec.Meta.Labels, sshKeyMeta.SshConfigMapName, metaAndSpec.Meta.Namespace, generator)
	if err != nil {
		return
	}
	log.Info().Msgf("Successful create config map %v", configMap.Name)

	pod, err := createAndGetPod(ctx, k, metaAndSpec, sshKeyMeta.SshConfigMapName, options)
	if err != nil {
		return
	}
	podIP, podName, credential = shadowResult(pod, generator)
	return
}

func createAndGetPod(ctx context.Context, k KubernetesInterface, metaAndSpec *PodMetaAndSpec, sshcm string, options *options.DaemonOptions) (*coreV1.Pod, error) {
	localIPAddress := util.GetOutboundIP()
	log.Debug().Msgf("Client address %s", localIPAddress)
	metaAndSpec.Meta.Labels[common.KtName] = metaAndSpec.Meta.Name

	err := k.CreateShadowPod(ctx, metaAndSpec, sshcm, options)
	if err != nil {
		return nil, err
	}

	log.Info().Msgf("Deploying shadow pod %s in namespace %s", metaAndSpec.Meta.Name, metaAndSpec.Meta.Namespace)

	return k.WaitPodReady(metaAndSpec.Meta.Name, metaAndSpec.Meta.Namespace)
}

func tryGetExistingShadowRelatedObjs(ctx context.Context, k KubernetesInterface, resourceMeta *ResourceMeta, sshKeyMeta *SSHkeyMeta) (pod *coreV1.Pod, generator *util.SSHGenerator, err error) {
	_, shadowError := k.GetPod(ctx, resourceMeta.Name, resourceMeta.Namespace)
	if shadowError != nil {
		return
	}

	configMap, configMapError := k.GetConfigMap(ctx, sshKeyMeta.SshConfigMapName, resourceMeta.Namespace)
	if configMapError != nil {
		err = errors.New("Found shadow pod but no configMap. Please delete the pod " + resourceMeta.Name)
		return
	}

	generator = util.NewSSHGenerator(configMap.Data[common.SshAuthPrivateKey], configMap.Data[common.SshAuthKey], sshKeyMeta.PrivateKeyPath)

	err = util.WritePrivateKey(generator.PrivateKeyPath, []byte(configMap.Data[common.SshAuthPrivateKey]))
	if err != nil {
		return
	}

	pod, err = getShadowPod(ctx, k, resourceMeta)
	return
}

func getShadowPod(ctx context.Context, k KubernetesInterface, resourceMeta *ResourceMeta) (pod *coreV1.Pod, err error) {
	podList, err := k.GetPodsByLabel(ctx, resourceMeta.Labels, resourceMeta.Namespace)
	if err != nil {
		return
	}
	if len(podList.Items) == 1 {
		log.Info().Msgf("Found shadow daemon pod, reuse it")
		if err = k.IncreaseRef(ctx, resourceMeta.Name, resourceMeta.Namespace); err != nil {
			return
		}
		return &(podList.Items[0]), nil
	} else if len(podList.Items) > 1 {
		err = errors.New("Found more than one pod with name " + resourceMeta.Name + ", please make sure these is only one in namespace " + resourceMeta.Namespace)
	}
	return
}

func shadowResult(pod *coreV1.Pod, generator *util.SSHGenerator) (string, string, *util.SSHCredential) {
	podIP := pod.Status.PodIP
	podName := pod.Name
	credential := util.NewDefaultSSHCredential()
	credential.PrivateKeyPath = generator.PrivateKeyPath
	return podIP, podName, credential
}
