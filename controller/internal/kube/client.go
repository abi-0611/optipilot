package kube

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

// Client exposes the Kubernetes operations the controller needs.
type Client interface {
	GetDeployment(ctx context.Context, namespace, name string) (*appsv1.Deployment, error)
	PatchReplicas(ctx context.Context, namespace, name string, replicas int32) error
	PatchResources(ctx context.Context, namespace, name, container, cpu, memory string) error
	WatchDeployments(ctx context.Context, namespace, labelSelector string, resync time.Duration) (cache.SharedIndexInformer, error)
	GetConfigMap(ctx context.Context, namespace, name string) (*corev1.ConfigMap, error)
}

type kubeClient struct {
	clientset kubernetes.Interface
	logger    *slog.Logger
}

// InitClient builds a Kubernetes client from in-cluster config first, then
// falls back to kubeconfig.
func InitClient(kubeconfigPath string, logger *slog.Logger) (Client, error) {
	if logger == nil {
		logger = slog.Default()
	}

	cfg, source, err := loadConfig(kubeconfigPath)
	if err != nil {
		return nil, err
	}

	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("kubernetes clientset init (%s): %w", source, err)
	}

	logger.Info("kubernetes client initialized", "config_source", source)
	return &kubeClient{clientset: clientset, logger: logger}, nil
}

func loadConfig(kubeconfigPath string) (*rest.Config, string, error) {
	// 1) In-cluster first.
	inCluster, inClusterErr := rest.InClusterConfig()
	if inClusterErr == nil {
		return inCluster, "in-cluster", nil
	}

	// 2) Explicit kubeconfig path, then $KUBECONFIG, then ~/.kube/config.
	path := strings.TrimSpace(kubeconfigPath)
	if path == "" {
		path = strings.TrimSpace(os.Getenv("KUBECONFIG"))
	}
	if path == "" {
		if home := homedir.HomeDir(); home != "" {
			path = filepath.Join(home, ".kube", "config")
		}
	}

	kubeCfg, kubeErr := clientcmd.BuildConfigFromFlags("", path)
	if kubeErr == nil {
		return kubeCfg, fmt.Sprintf("kubeconfig:%s", path), nil
	}

	return nil, "", fmt.Errorf(
		"kubernetes config unavailable: in-cluster error=%v; kubeconfig error=%v",
		inClusterErr, kubeErr,
	)
}

func (c *kubeClient) GetDeployment(ctx context.Context, namespace, name string) (*appsv1.Deployment, error) {
	dep, err := c.clientset.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get deployment %s/%s: %w", namespace, name, err)
	}
	return dep, nil
}

func (c *kubeClient) PatchReplicas(ctx context.Context, namespace, name string, replicas int32) error {
	patch := map[string]any{
		"spec": map[string]any{
			"replicas": replicas,
		},
	}
	body, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("marshal replicas patch: %w", err)
	}

	if _, err := c.clientset.AppsV1().Deployments(namespace).Patch(
		ctx,
		name,
		types.StrategicMergePatchType,
		body,
		metav1.PatchOptions{},
	); err != nil {
		return fmt.Errorf("patch replicas for %s/%s to %d: %w", namespace, name, replicas, err)
	}
	return nil
}

func (c *kubeClient) PatchResources(ctx context.Context, namespace, name, container, cpu, memory string) error {
	if container == "" {
		return fmt.Errorf("patch resources for %s/%s: empty container name", namespace, name)
	}
	if cpu == "" || memory == "" {
		return fmt.Errorf("patch resources for %s/%s: cpu and memory are required", namespace, name)
	}

	patch := map[string]any{
		"spec": map[string]any{
			"template": map[string]any{
				"spec": map[string]any{
					"containers": []map[string]any{
						{
							"name": container,
							"resources": map[string]any{
								"requests": map[string]string{
									"cpu":    cpu,
									"memory": memory,
								},
								"limits": map[string]string{
									"cpu":    cpu,
									"memory": memory,
								},
							},
						},
					},
				},
			},
		},
	}
	body, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("marshal resources patch: %w", err)
	}

	if _, err := c.clientset.AppsV1().Deployments(namespace).Patch(
		ctx,
		name,
		types.StrategicMergePatchType,
		body,
		metav1.PatchOptions{},
	); err != nil {
		return fmt.Errorf("patch resources for %s/%s: %w", namespace, name, err)
	}
	return nil
}

func (c *kubeClient) WatchDeployments(
	ctx context.Context,
	namespace string,
	labelSelector string,
	resync time.Duration,
) (cache.SharedIndexInformer, error) {
	if resync <= 0 {
		resync = 30 * time.Second
	}

	opts := make([]informers.SharedInformerOption, 0, 2)
	ns := strings.TrimSpace(namespace)
	if ns != "" && ns != "*" && ns != "all" {
		opts = append(opts, informers.WithNamespace(ns))
	}
	if labelSelector != "" {
		opts = append(opts, informers.WithTweakListOptions(func(lo *metav1.ListOptions) {
			lo.LabelSelector = labelSelector
		}))
	}

	factory := informers.NewSharedInformerFactoryWithOptions(c.clientset, resync, opts...)
	informer := factory.Apps().V1().Deployments().Informer()
	factory.Start(ctx.Done())
	return informer, nil
}

func (c *kubeClient) GetConfigMap(ctx context.Context, namespace, name string) (*corev1.ConfigMap, error) {
	cm, err := c.clientset.CoreV1().ConfigMaps(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get configmap %s/%s: %w", namespace, name, err)
	}
	return cm, nil
}
