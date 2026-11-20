package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
)

// Supplier attachments go to an S3-compatible bucket configured only through
// environment variables, so the credentials never reach the database or UI:
//
//	SUPPLIER_S3_ENDPOINT           e.g. https://s3.ap-southeast-1.amazonaws.com
//	SUPPLIER_S3_REGION             default us-east-1
//	SUPPLIER_S3_BUCKET
//	SUPPLIER_S3_ACCESS_KEY_ID
//	SUPPLIER_S3_SECRET_ACCESS_KEY
//	SUPPLIER_S3_SESSION_TOKEN      optional
//	SUPPLIER_S3_FORCE_PATH_STYLE   true for MinIO and most self-hosted stores
//	SUPPLIER_S3_KEY_PREFIX         default suppliers/
//	SUPPLIER_ATTACHMENT_MAX_MB     default 20
type supplierStorageConfig struct {
	endpoint     *url.URL
	region       string
	bucket       string
	credentials  aws.Credentials
	forcePath    bool
	keyPrefix    string
	maxSizeBytes int64
}

// SupplierAttachmentTypes maps the accepted file extensions to the content
// type stored with the object.
var SupplierAttachmentTypes = map[string]string{
	".pdf":  "application/pdf",
	".doc":  "application/msword",
	".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	".xls":  "application/vnd.ms-excel",
	".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".zip":  "application/zip",
}

var supplierStorageClient = &http.Client{Timeout: 2 * time.Minute}

func loadSupplierStorage() (*supplierStorageConfig, bool) {
	rawEndpoint := strings.TrimRight(common.GetEnvOrDefaultString("SUPPLIER_S3_ENDPOINT", ""), "/")
	bucket := common.GetEnvOrDefaultString("SUPPLIER_S3_BUCKET", "")
	accessKey := common.GetEnvOrDefaultString("SUPPLIER_S3_ACCESS_KEY_ID", "")
	secretKey := common.GetEnvOrDefaultString("SUPPLIER_S3_SECRET_ACCESS_KEY", "")
	if rawEndpoint == "" || bucket == "" || accessKey == "" || secretKey == "" {
		return nil, false
	}
	endpoint, err := url.Parse(rawEndpoint)
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "https" && endpoint.Scheme != "http") {
		common.SysError(common.LogText("invalid SUPPLIER_S3_ENDPOINT: %s", rawEndpoint))
		return nil, false
	}
	prefix := strings.Trim(common.GetEnvOrDefaultString("SUPPLIER_S3_KEY_PREFIX", "suppliers"), "/")
	return &supplierStorageConfig{
		endpoint: endpoint,
		region:   common.GetEnvOrDefaultString("SUPPLIER_S3_REGION", "us-east-1"),
		bucket:   bucket,
		credentials: aws.Credentials{
			AccessKeyID:     accessKey,
			SecretAccessKey: secretKey,
			SessionToken:    common.GetEnvOrDefaultString("SUPPLIER_S3_SESSION_TOKEN", ""),
		},
		forcePath:    common.GetEnvOrDefaultBool("SUPPLIER_S3_FORCE_PATH_STYLE", false),
		keyPrefix:    prefix,
		maxSizeBytes: int64(max(1, common.GetEnvOrDefault("SUPPLIER_ATTACHMENT_MAX_MB", 20))) << 20,
	}, true
}

func SupplierStorageEnabled() bool {
	_, ok := loadSupplierStorage()
	return ok
}

// SupplierAttachmentMaxBytes is the upload limit, also when storage is off.
func SupplierAttachmentMaxBytes() int64 {
	return int64(max(1, common.GetEnvOrDefault("SUPPLIER_ATTACHMENT_MAX_MB", 20))) << 20
}

// objectURL builds the object address. Keys only hold [a-z0-9/._-], so they
// need no escaping and the signer can sign the path as is.
func (cfg *supplierStorageConfig) objectURL(key string) string {
	if cfg.forcePath {
		return cfg.endpoint.Scheme + "://" + cfg.endpoint.Host + path.Join("/", cfg.endpoint.Path, cfg.bucket, key)
	}
	return cfg.endpoint.Scheme + "://" + cfg.bucket + "." + cfg.endpoint.Host + path.Join("/", cfg.endpoint.Path, key)
}

func (cfg *supplierStorageConfig) signer() *v4.Signer {
	return v4.NewSigner(func(o *v4.SignerOptions) { o.DisableURIPathEscaping = true })
}

// NewSupplierObjectKey returns a random key under the target's folder. The
// original file name is kept in the database, never in the key.
func NewSupplierObjectKey(bizType string, bizID int, ext string) (string, error) {
	cfg, ok := loadSupplierStorage()
	if !ok {
		return "", common.NewMessage("Attachment storage is not configured")
	}
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	month := time.Now().UTC().Format("200601")
	return fmt.Sprintf("%s/%s/%d/%s/%s%s", cfg.keyPrefix, bizType, bizID, month, hex.EncodeToString(random), ext), nil
}

// PutSupplierObject uploads body with a signed payload hash.
func PutSupplierObject(ctx context.Context, key, contentType string, body []byte) error {
	cfg, ok := loadSupplierStorage()
	if !ok {
		return common.NewMessage("Attachment storage is not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, cfg.objectURL(key), bytes.NewReader(body))
	if err != nil {
		return err
	}
	digest := sha256.Sum256(body)
	payloadHash := hex.EncodeToString(digest[:])
	req.ContentLength = int64(len(body))
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)
	if err := cfg.signer().SignHTTP(ctx, cfg.credentials, req, payloadHash, "s3", cfg.region, time.Now()); err != nil {
		return err
	}
	resp, err := supplierStorageClient.Do(req)
	if err != nil {
		common.SysError(common.LogText("supplier attachment upload failed: %s", err.Error()))
		return common.NewMessage("Failed to store the attachment")
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		common.SysError(common.LogText("supplier attachment upload failed: status %d: %s", resp.StatusCode, string(detail)))
		return common.NewMessage("Failed to store the attachment")
	}
	return nil
}

// PresignSupplierObject returns a GET URL valid for expires that downloads the
// object under fileName.
func PresignSupplierObject(ctx context.Context, key, fileName string, expires time.Duration) (string, error) {
	cfg, ok := loadSupplierStorage()
	if !ok {
		return "", common.NewMessage("Attachment storage is not configured")
	}
	query := url.Values{}
	query.Set("X-Amz-Expires", fmt.Sprint(int(expires.Seconds())))
	query.Set("response-content-disposition", "attachment; filename*=UTF-8''"+url.PathEscape(fileName))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.objectURL(key)+"?"+query.Encode(), nil)
	if err != nil {
		return "", err
	}
	signed, _, err := cfg.signer().PresignHTTP(ctx, cfg.credentials, req, "UNSIGNED-PAYLOAD", "s3", cfg.region, time.Now())
	return signed, err
}
