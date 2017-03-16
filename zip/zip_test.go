package zip

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	"github.com/Sirupsen/logrus"
)

const (
	f1 = "A nice walking dead is coming through the storm"
)

func TestPrepareArtifacts(t *testing.T) {
	if _, err := os.Open("tempdir"); err != nil {
		err := os.Mkdir("tempdir", os.ModeDir)
		if err != nil {
			t.Error(err)
		}
	}
	first, err := os.OpenFile(filepath.Join("tempdir", "f1.txt"), os.O_RDWR|os.O_TRUNC|os.O_CREATE, os.ModePerm)
	defer first.Close()
	if err != nil {
		t.Error(err)
	}
	first.Write([]byte(f1))
	if e := first.Sync(); e != nil {
		t.Error(e)
	}
}

func TestAddZip(t *testing.T) {
	zf, err := os.OpenFile(filepath.Join("tempdir", "temp.zip"), os.O_RDWR|os.O_TRUNC|os.O_CREATE, os.ModePerm)
	defer zf.Close()
	if err != nil {
		t.Error(err)
	}
	z := zip.NewWriter(zf)
	if e := AddToZip(z, "tempdir", "tempdir", &logrus.Logger{}); e != nil {
		t.Error(e)
	}
}

func TestCleanup(t *testing.T) {
	if err := os.RemoveAll("tempdir"); err != nil {
		t.Error(err)
	}
}
