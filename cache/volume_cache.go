package cache

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/pkg/errors"

	"github.com/buildpacks/lifecycle/platform"
)

type VolumeCache struct {
	committed    bool
	dir          string
	backupDir    string
	stagingDir   string
	committedDir string
}

func NewVolumeCache(dir string) (*VolumeCache, error) {
	if _, err := os.Stat(dir); err != nil {
		return nil, err
	}

	c := &VolumeCache{
		dir:          dir,
		backupDir:    filepath.Join(dir, "committed-backup"),
		stagingDir:   filepath.Join(dir, "staging"),
		committedDir: filepath.Join(dir, "committed"),
	}

	if err := c.setupStagingDir(); err != nil {
		return nil, errors.Wrapf(err, "initializing staging directory '%s'", c.stagingDir)
	}

	if err := os.RemoveAll(c.backupDir); err != nil {
		return nil, errors.Wrapf(err, "removing backup directory '%s'", c.backupDir)
	}

	if err := os.MkdirAll(c.committedDir, 0777); err != nil {
		return nil, errors.Wrapf(err, "creating committed directory '%s'", c.committedDir)
	}

	return c, nil
}

func (c *VolumeCache) Exists() bool {
	if _, err := os.Stat(c.committedDir); err != nil {
		return false
	}
	return true
}

func (c *VolumeCache) Name() string {
	return c.dir
}

func (c *VolumeCache) SetMetadata(metadata platform.CacheMetadata) error {
	if c.committed {
		return errCacheCommitted
	}
	metadataPath := filepath.Join(c.stagingDir, MetadataLabel)
	file, err := os.Create(metadataPath)
	if err != nil {
		return errors.Wrapf(err, "creating metadata file '%s'", metadataPath)
	}
	defer file.Close()

	if err := json.NewEncoder(file).Encode(metadata); err != nil {
		return errors.Wrap(err, "marshalling metadata")
	}

	return nil
}

func (c *VolumeCache) RetrieveMetadata() (platform.CacheMetadata, error) {
	metadataPath := filepath.Join(c.committedDir, MetadataLabel)
	file, err := os.Open(metadataPath)
	if err != nil {
		if os.IsNotExist(err) {
			return platform.CacheMetadata{}, nil
		}
		return platform.CacheMetadata{}, errors.Wrapf(err, "opening metadata file '%s'", metadataPath)
	}
	defer file.Close()

	metadata := platform.CacheMetadata{}
	if json.NewDecoder(file).Decode(&metadata) != nil {
		return platform.CacheMetadata{}, nil
	}
	return metadata, nil
}

func (c *VolumeCache) AddLayerFile(tarPath string, diffID string) error {
	if c.committed {
		return errCacheCommitted
	}
	layerTar := diffIDPath(c.stagingDir, diffID)
	if _, err := os.Stat(layerTar); err == nil {
		// don't waste time rewriting an identical layer
		return nil
	}

	if err := copyFile(tarPath, layerTar); err != nil {
		return errors.Wrapf(err, "caching layer (%s)", diffID)
	}
	return nil
}

func (c *VolumeCache) AddLayer(rc io.ReadCloser, diffID string) error {
	if c.committed {
		return errCacheCommitted
	}

	fh, err := os.Create(diffIDPath(c.stagingDir, diffID))
	if err != nil {
		return errors.Wrapf(err, "create layer file in cache")
	}
	defer fh.Close()

	if _, err := io.Copy(fh, rc); err != nil {
		return errors.Wrap(err, "copying layer to tar file")
	}
	return nil
}

func (c *VolumeCache) ReuseLayer(diffID string) error {
	if c.committed {
		return errCacheCommitted
	}
	if err := os.Link(diffIDPath(c.committedDir, diffID), diffIDPath(c.stagingDir, diffID)); err != nil && !os.IsExist(err) {
		return errors.Wrapf(err, "reusing layer (%s)", diffID)
	}
	return nil
}

func (c *VolumeCache) RetrieveLayer(diffID string) (io.ReadCloser, error) {
	path, err := c.RetrieveLayerFile(diffID)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.Wrapf(err, "opening layer with SHA '%s'", diffID)
	}
	return file, nil
}

func (c *VolumeCache) HasLayer(diffID string) (bool, error) {
	if _, err := os.Stat(diffIDPath(c.committedDir, diffID)); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, errors.Wrapf(err, "retrieving layer with SHA '%s'", diffID)
	}
	return true, nil
}

func (c *VolumeCache) RetrieveLayerFile(diffID string) (string, error) {
	path := diffIDPath(c.committedDir, diffID)
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return "", errors.Wrapf(err, "layer with SHA '%s' not found", diffID)
		}
		return "", errors.Wrapf(err, "retrieving layer with SHA '%s'", diffID)
	}
	return path, nil
}

func (c *VolumeCache) Commit() error {
	if c.committed {
		return errCacheCommitted
	}
	c.committed = true
	if err := os.Rename(c.committedDir, c.backupDir); err != nil {
		return errors.Wrap(err, "backing up cache")
	}
	defer os.RemoveAll(c.backupDir)

	if err1 := os.Rename(c.stagingDir, c.committedDir); err1 != nil {
		if err2 := os.Rename(c.backupDir, c.committedDir); err2 != nil {
			return errors.Wrap(err2, "rolling back cache")
		}
		return errors.Wrap(err1, "committing cache")
	}

	return nil
}

func diffIDPath(basePath, diffID string) string {
	if runtime.GOOS == "windows" {
		// Avoid colons in Windows file paths
		diffID = strings.TrimPrefix(diffID, "sha256:")
	}
	return filepath.Join(basePath, diffID+".tar")
}

func (c *VolumeCache) setupStagingDir() error {
	if err := os.RemoveAll(c.stagingDir); err != nil {
		return err
	}
	return os.MkdirAll(c.stagingDir, 0777)
}

func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(to)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)

	return err
}
