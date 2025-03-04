/*
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

package kubernetes

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	kclient "sigs.k8s.io/controller-runtime/pkg/client"

	esv1beta1 "github.com/external-secrets/external-secrets/apis/externalsecrets/v1beta1"
	esmeta "github.com/external-secrets/external-secrets/apis/meta/v1"
	"github.com/external-secrets/external-secrets/pkg/provider"
	"github.com/external-secrets/external-secrets/pkg/provider/schema"
	"github.com/external-secrets/external-secrets/pkg/utils"
)

const (
	errPropertyNotFound                    = "property field not found on extrenal secrets"
	errKubernetesCredSecretName            = "kubernetes credentials are empty"
	errInvalidClusterStoreMissingNamespace = "invalid clusterStore missing Cert namespace"
	errFetchCredentialsSecret              = "could not fetch Credentials secret: %w"
	errMissingCredentials                  = "missing Credentials: %v"
	errUninitalizedKubernetesProvider      = "provider kubernetes is not initialized"
	errEmptyKey                            = "key %s found but empty"
)

type KClient interface {
	Get(ctx context.Context, name string, opts metav1.GetOptions) (*corev1.Secret, error)
}

// ProviderKubernetes is a provider for Kubernetes.
type ProviderKubernetes struct {
	Client KClient
}

var _ provider.SecretsClient = &ProviderKubernetes{}

type BaseClient struct {
	kube        kclient.Client
	store       *esv1beta1.KubernetesProvider
	namespace   string
	storeKind   string
	Certificate []byte
	Key         []byte
	CA          []byte
	BearerToken []byte
}

func init() {
	schema.Register(&ProviderKubernetes{}, &esv1beta1.SecretStoreProvider{
		Kubernetes: &esv1beta1.KubernetesProvider{},
	})
}

// NewClient constructs a Kubernetes Provider.
func (k *ProviderKubernetes) NewClient(ctx context.Context, store esv1beta1.GenericStore, kube kclient.Client, namespace string) (provider.SecretsClient, error) {
	storeSpec := store.GetSpec()
	if storeSpec == nil || storeSpec.Provider == nil || storeSpec.Provider.Kubernetes == nil {
		return nil, fmt.Errorf("no store type or wrong store type")
	}
	storeSpecKubernetes := storeSpec.Provider.Kubernetes

	bStore := BaseClient{
		kube:      kube,
		store:     storeSpecKubernetes,
		namespace: namespace,
		storeKind: store.GetObjectKind().GroupVersionKind().Kind,
	}

	if err := bStore.setAuth(ctx); err != nil {
		return nil, err
	}

	config := &rest.Config{
		Host:        bStore.store.Server.URL,
		BearerToken: string(bStore.BearerToken),
		TLSClientConfig: rest.TLSClientConfig{
			Insecure: false,
			CertData: bStore.Certificate,
			KeyData:  bStore.Key,
			CAData:   bStore.CA,
		},
	}

	kubeClientSet, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("error configuring clientset: %w", err)
	}

	k.Client = kubeClientSet.CoreV1().Secrets(bStore.store.RemoteNamespace)

	return k, nil
}

func (k *ProviderKubernetes) Close(ctx context.Context) error {
	return nil
}

func (k *ProviderKubernetes) GetSecret(ctx context.Context, ref esv1beta1.ExternalSecretDataRemoteRef) ([]byte, error) {
	if ref.Property == "" {
		return nil, fmt.Errorf(errPropertyNotFound)
	}

	payload, err := k.GetSecretMap(ctx, ref)

	if err != nil {
		return nil, err
	}

	val, ok := payload[ref.Property]
	if !ok {
		return nil, fmt.Errorf("property %s does not exist in key %s", ref.Property, ref.Key)
	}
	return val, nil
}

func (k *ProviderKubernetes) GetSecretMap(ctx context.Context, ref esv1beta1.ExternalSecretDataRemoteRef) (map[string][]byte, error) {
	if utils.IsNil(k.Client) {
		return nil, fmt.Errorf(errUninitalizedKubernetesProvider)
	}
	opts := metav1.GetOptions{}
	secretOut, err := k.Client.Get(ctx, ref.Key, opts)

	if err != nil {
		return nil, err
	}

	var payload map[string][]byte
	if len(secretOut.Data) != 0 {
		payload = secretOut.Data
	}

	return payload, nil
}

func (k *ProviderKubernetes) GetAllSecrets(ctx context.Context, ref esv1beta1.ExternalSecretFind) (map[string][]byte, error) {
	return nil, fmt.Errorf("not implemented")
}

func (k *BaseClient) setAuth(ctx context.Context) error {
	var err error
	if len(k.store.Server.CABundle) > 0 {
		k.CA = k.store.Server.CABundle
	} else if k.store.Server.CAProvider != nil {
		keySelector := esmeta.SecretKeySelector{
			Name:      k.store.Server.CAProvider.Name,
			Namespace: k.store.Server.CAProvider.Namespace,
			Key:       k.store.Server.CAProvider.Key,
		}
		k.CA, err = k.fetchSecretKey(ctx, keySelector, "CA")
		if err != nil {
			return err
		}
	} else {
		return fmt.Errorf("no Certificate Authority provided")
	}

	if k.store.Auth.Token != nil {
		k.BearerToken, err = k.fetchSecretKey(ctx, k.store.Auth.Token.BearerToken, "bearerToken")
		if err != nil {
			return err
		}
	} else if k.store.Auth.ServiceAccount != nil {
		return fmt.Errorf("not implemented yet")
	} else if k.store.Auth.Cert != nil {
		k.Certificate, err = k.fetchSecretKey(ctx, k.store.Auth.Cert.ClientCert, "cert")
		if err != nil {
			return err
		}
		k.Key, err = k.fetchSecretKey(ctx, k.store.Auth.Cert.ClientKey, "key")
		if err != nil {
			return err
		}
	} else {
		return fmt.Errorf("no credentials provided")
	}

	return nil
}

func (k *BaseClient) fetchSecretKey(ctx context.Context, key esmeta.SecretKeySelector, component string) ([]byte, error) {
	keySecret := &corev1.Secret{}
	keySecretName := key.Name
	if keySecretName == "" {
		return nil, fmt.Errorf(errKubernetesCredSecretName)
	}
	objectKey := types.NamespacedName{
		Name:      keySecretName,
		Namespace: k.namespace,
	}
	// only ClusterStore is allowed to set namespace (and then it's required)
	if k.storeKind == esv1beta1.ClusterSecretStoreKind {
		if key.Namespace == nil {
			return nil, fmt.Errorf(errInvalidClusterStoreMissingNamespace)
		}
		objectKey.Namespace = *key.Namespace
	}

	err := k.kube.Get(ctx, objectKey, keySecret)
	if err != nil {
		return nil, fmt.Errorf(errFetchCredentialsSecret, err)
	}

	val, ok := keySecret.Data[key.Key]
	if !ok {
		return nil, fmt.Errorf(errMissingCredentials, component)
	}

	if len(val) == 0 {
		return nil, fmt.Errorf(errEmptyKey, component)
	}
	return val, nil
}

func (k *ProviderKubernetes) Validate() error {
	return nil
}
