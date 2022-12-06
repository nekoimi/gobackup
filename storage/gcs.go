package storage

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cloud.google.com/go/storage"
	"github.com/hako/durafmt"
	"github.com/huacnlee/gobackup/logger"
	"google.golang.org/api/option"
)

// GCS - Google Clound storage
//
// type: gcs
// bucket: gobackup-test
// path: backups
// credentials: { ... }
// timeout: 300
type GCS struct {
	Base
	bucket  string
	path    string
	timeout time.Duration
	client  *storage.Client
}

func (s *GCS) open() (err error) {
	// https://cloud.google.com/storage/docs/locations
	s.viper.SetDefault("timeout", "300")

	timeout := s.viper.GetInt("timeout")
	s.timeout = time.Duration(timeout) * time.Second
	s.path = s.viper.GetString("path")
	s.bucket = s.viper.GetString("bucket")

	credentials := s.viper.GetString("credentials")

	s.client, err = storage.NewClient(context.Background(), option.WithCredentialsJSON([]byte(credentials)))
	if err != nil {
		return err
	}

	return
}

func (s *GCS) close() {
	s.client.Close()
}

func (s *GCS) upload(fileKey string) (err error) {
	logger := logger.Tag("GCS")

	var ctx = context.Background()
	var cancel context.CancelFunc

	if s.timeout.Seconds() > 0 {
		logger.Info(fmt.Sprintf("timeout: %s", s.timeout))
		ctx, cancel = context.WithTimeout(ctx, s.timeout)
		defer cancel()
	}

	var fileKeys []string
	if len(s.fileKeys) != 0 {
		// directory
		// 2022.12.04.07.09.47/2022.12.04.07.09.47.tar.xz-000
		fileKeys = s.fileKeys
	} else {
		// file
		// 2022.12.04.07.09.25.tar.xz
		fileKeys = append(fileKeys, fileKey)
	}

	for _, key := range fileKeys {
		filePath := filepath.Join(filepath.Dir(s.archivePath), key)
		// Open file
		f, err := os.Open(filePath)
		if err != nil {
			return fmt.Errorf("GCS failed to open file %q, %v", filePath, err)
		}
		defer f.Close()

		info, err := f.Stat()
		if err != nil {
			return fmt.Errorf("GCS failed to get size of file %q, %v", filePath, err)
		}

		remotePath := filepath.Join(s.path, key)
		object := s.client.Bucket(s.bucket).Object(remotePath).If(storage.Conditions{DoesNotExist: true})
		writer := object.NewWriter(ctx)

		logger.Info(fmt.Sprintf("-> Uploading %s (%d MiB)...", remotePath, info.Size()/(1024*1024)))

		start := time.Now()

		if _, err = io.Copy(writer, f); err != nil {
			return fmt.Errorf("GCS upload error: %v", err)
		}
		if err := writer.Close(); err != nil {
			return fmt.Errorf("GCS upload Writer.Close: %v", err)
		}

		t := time.Now()
		elapsed := t.Sub(start)

		rate := math.Ceil(float64(info.Size()) / (elapsed.Seconds() * 1024 * 1024))

		logger.Info(fmt.Sprintf("Duration %v, rate %.1f MiB/s", durafmt.Parse(elapsed).LimitFirstN(2).String(), rate))
	}

	return nil
}

func (s *GCS) delete(fileKey string) (err error) {
	// No need to remove empty directory
	if !strings.HasSuffix(fileKey, "/") {
		remotePath := filepath.Join(s.path, fileKey)
		object := s.client.Bucket(s.bucket).Object(remotePath)
		if err = object.Delete(context.Background()); err != nil {
			return fmt.Errorf("GCS failed to delete file %q, %v", remotePath, err)
		}
	}

	return nil
}
