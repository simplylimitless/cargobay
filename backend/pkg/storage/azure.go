package storage

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/bloberror"
)

// AzureAdapter implements Azure Blob Storage with hot/cold/balanced tiers
type AzureAdapter struct {
	client    *azblob.Client
	container string
	prefix    string
	tiering   string
}

// NewAzureAdapter creates a new Azure Blob Storage adapter
func NewAzureAdapter(config map[string]string) (*AzureAdapter, error) {
	container := config["container"]
	if container == "" {
		return nil, fmt.Errorf("container name is required for Azure storage")
	}

	prefix := config["prefix"]
	tiering := config["tiering"]
	if tiering == "" {
		tiering = "Hot"
	}

	var client *azblob.Client
	var err error

	// Connection string authentication
	if connString := config["connection_string"]; connString != "" {
		client, err = azblob.NewClientFromConnectionString(connString, nil)
	} else if config["account_key"] != "" {
		// Account key authentication
		var credential *azblob.SharedKeyCredential
		credential, err = azblob.NewSharedKeyCredential(config["account_name"], config["account_key"])
		if err != nil {
			return nil, fmt.Errorf("failed to create credentials: %w", err)
		}
		client, err = azblob.NewClientWithSharedKeyCredential(config["account_url"], credential, nil)
	} else if config["sas_token"] != "" {
		// SAS token authentication: the SAS token is appended as a query string
		// to the account URL, then the client is created with no credential.
		sasURL := strings.TrimSuffix(config["account_url"], "/") + "?" + strings.TrimPrefix(config["sas_token"], "?")
		client, err = azblob.NewClientWithNoCredential(sasURL, nil)
	} else {
		// Managed identity or Azure CLI
		var credential *azidentity.DefaultAzureCredential
		credential, err = azidentity.NewDefaultAzureCredential(nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create default credential: %w", err)
		}
		client, err = azblob.NewClient(config["account_url"], credential, nil)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create Azure client: %w", err)
	}

	return &AzureAdapter{
		client:    client,
		container: container,
		prefix:    prefix,
		tiering:   tiering,
	}, nil
}

// Connect establishes connection to Azure Blob Storage
func (a *AzureAdapter) Connect() error {
	_, err := a.client.ServiceClient().NewContainerClient(a.container).GetProperties(context.Background(), nil)
	return err
}

// Disconnect closes connections
func (a *AzureAdapter) Disconnect() error {
	return nil
}

// getStoragePath constructs the blob path for an artifact
func (a *AzureAdapter) getStoragePath(registryID, namespace, artifactName, version string) string {
	path := fmt.Sprintf("%s/%s/%s/%s/%s", registryID, namespace, artifactName, version, "artifact.bin")
	if a.prefix != "" {
		path = fmt.Sprintf("%s/%s", a.prefix, path)
	}
	return path
}

// SaveArtifact saves an artifact to Azure Blob Storage
func (a *AzureAdapter) SaveArtifact(registryID, namespace, artifactName, version string, data []byte) (string, error) {
	key := a.getStoragePath(registryID, namespace, artifactName, version)

	// Determine access tier
	tier := blob.AccessTierHot
	switch a.tiering {
	case "Cool":
		tier = blob.AccessTierCool
	case "Archive":
		tier = blob.AccessTierArchive
	}

	// Set metadata
	meta := map[string]*string{
		"Registry":     &registryID,
		"Namespace":    &namespace,
		"ArtifactName": &artifactName,
		"Version":      &version,
	}

	_, err := a.client.UploadBuffer(context.Background(), a.container, key, data, &azblob.UploadBufferOptions{
		AccessTier: &tier,
		Metadata:   meta,
	})
	if err != nil {
		return "", fmt.Errorf("failed to upload to Azure: %w", err)
	}

	return key, nil
}

// SaveArtifactStream saves an artifact to Azure Blob Storage by streaming
// directly from r, without buffering the whole payload in memory first.
func (a *AzureAdapter) SaveArtifactStream(registryID, namespace, artifactName, version string, r io.Reader) (string, error) {
	key := a.getStoragePath(registryID, namespace, artifactName, version)

	tier := blob.AccessTierHot
	switch a.tiering {
	case "Cool":
		tier = blob.AccessTierCool
	case "Archive":
		tier = blob.AccessTierArchive
	}

	meta := map[string]*string{
		"Registry":     &registryID,
		"Namespace":    &namespace,
		"ArtifactName": &artifactName,
		"Version":      &version,
	}

	_, err := a.client.UploadStream(context.Background(), a.container, key, r, &azblob.UploadStreamOptions{
		AccessTier: &tier,
		Metadata:   meta,
	})
	if err != nil {
		return "", fmt.Errorf("failed to upload to Azure: %w", err)
	}

	return key, nil
}

// GetArtifactStream opens an artifact from Azure Blob Storage for
// streaming. Returns (nil, nil) if it isn't cached.
func (a *AzureAdapter) GetArtifactStream(registryID, namespace, artifactName, version string) (io.ReadCloser, error) {
	key := a.getStoragePath(registryID, namespace, artifactName, version)

	resp, err := a.client.DownloadStream(context.Background(), a.container, key, nil)
	if err != nil {
		if bloberror.HasCode(err, bloberror.BlobNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return resp.Body, nil
}

// GetArtifact retrieves an artifact from Azure Blob Storage
func (a *AzureAdapter) GetArtifact(registryID, namespace, artifactName, version string) ([]byte, error) {
	key := a.getStoragePath(registryID, namespace, artifactName, version)

	resp, err := a.client.DownloadStream(context.Background(), a.container, key, nil)
	if err != nil {
		if bloberror.HasCode(err, bloberror.BlobNotFound) {
			return nil, nil
		}
		return nil, err
	}
	defer resp.Body.Close()

	return io.ReadAll(resp.Body)
}

// DeleteArtifact removes an artifact from Azure Blob Storage
func (a *AzureAdapter) DeleteArtifact(registryID, namespace, artifactName, version string) error {
	key := a.getStoragePath(registryID, namespace, artifactName, version)

	_, err := a.client.DeleteBlob(context.Background(), a.container, key, nil)
	return err
}

// ArtifactExists checks if an artifact exists in Azure Blob Storage
func (a *AzureAdapter) ArtifactExists(registryID, namespace, artifactName, version string) (bool, error) {
	key := a.getStoragePath(registryID, namespace, artifactName, version)

	_, err := a.client.ServiceClient().NewContainerClient(a.container).NewBlobClient(key).GetProperties(context.Background(), nil)
	if err != nil {
		if bloberror.HasCode(err, bloberror.BlobNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Upload uploads a file to Azure Blob Storage
func (a *AzureAdapter) Upload(bucket, key string, data []byte, contentType string) error {
	if bucket == "" {
		bucket = a.container
	}

	_, err := a.client.UploadBuffer(context.Background(), bucket, key, data, nil)
	return err
}

// Download downloads a file from Azure Blob Storage
func (a *AzureAdapter) Download(bucket, key string) ([]byte, error) {
	if bucket == "" {
		bucket = a.container
	}

	resp, err := a.client.DownloadStream(context.Background(), bucket, key, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return io.ReadAll(resp.Body)
}

// DeleteFile deletes a file from Azure Blob Storage
func (a *AzureAdapter) DeleteFile(bucket, key string) error {
	if bucket == "" {
		bucket = a.container
	}

	_, err := a.client.DeleteBlob(context.Background(), bucket, key, nil)
	return err
}

// ListFiles lists files in Azure Blob Storage with prefix
func (a *AzureAdapter) ListFiles(bucket, prefix string) ([]string, error) {
	if bucket == "" {
		bucket = a.container
	}

	var keys []string
	pager := a.client.NewListBlobsFlatPager(bucket, &azblob.ListBlobsFlatOptions{
		Prefix: &prefix,
	})

	for pager.More() {
		resp, err := pager.NextPage(context.Background())
		if err != nil {
			return nil, err
		}

		for _, item := range resp.Segment.BlobItems {
			if item.Name != nil {
				keys = append(keys, *item.Name)
			}
		}
	}

	return keys, nil
}
