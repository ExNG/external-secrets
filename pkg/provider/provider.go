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

package provider

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"

	esv1beta1 "github.com/external-secrets/external-secrets/apis/externalsecrets/v1beta1"
)

// Provider is a common interface for interacting with secret backends.
type Provider interface {
	// NewClient constructs a SecretsManager Provider
	NewClient(ctx context.Context, store esv1beta1.GenericStore, kube client.Client, namespace string) (SecretsClient, error)
}

// SecretsClient provides access to secrets.
type SecretsClient interface {
	// GetSecret returns a single secret from the provider
	GetSecret(ctx context.Context, ref esv1beta1.ExternalSecretDataRemoteRef) ([]byte, error)

	// Validate checks if the client is configured correctly
	// and is able to retrieve secrets from the provider
	Validate() error

	// GetSecretMap returns multiple k/v pairs from the provider
	GetSecretMap(ctx context.Context, ref esv1beta1.ExternalSecretDataRemoteRef) (map[string][]byte, error)

	// GetAllSecrets returns multiple k/v pairs from the provider
	GetAllSecrets(ctx context.Context, ref esv1beta1.ExternalSecretFind) (map[string][]byte, error)

	Close(ctx context.Context) error
}
