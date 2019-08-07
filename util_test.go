package adbfs

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"log"
	"os"
	"path"
	"testing"

	"github.com/Sirupsen/logrus"
	"github.com/hanwen/go-fuse/fuse/nodefs"
	"github.com/sebastianhaberey/adbfs/internal/cli"
	"github.com/stretchr/testify/assert"
	"github.com/zach-klippenstein/goadb"
)

func init() {
	// Disable most logging when running tests.
	cli.Log.Level = logrus.WarnLevel
}

func TestAsFuseDirEntriesNoErr(t *testing.T) {
	entries := []*adb.DirEntry{
		&adb.DirEntry{
			Name: "/foo.txt",
			Size: 24,
			Mode: 0444,
		},
		&adb.DirEntry{
			Name: "/bar.txt",
			Size: 42,
			Mode: 0444,
		},
	}

	fuseEntries := asFuseDirEntries(entries)
	assert.Len(t, fuseEntries, 2)
	assert.Equal(t, "/foo.txt", fuseEntries[0].Name)
	assert.NotEqual(t, 0, fuseEntries[0].Mode)
	assert.Equal(t, "/bar.txt", fuseEntries[1].Name)
	assert.NotEqual(t, 0, fuseEntries[1].Mode)
}

func TestSummarizeByteSlicesForLog(t *testing.T) {
	vals := []interface{}{
		"foo",
		[]byte("bar"),
		42,
	}

	summarizeForLog(vals)

	assert.Equal(t, "foo", vals[0])
	assert.Equal(t, []interface{}{
		"foo",
		"[]byte(3)",
		42,
	}, vals)
}

func TestLoggingFile(t *testing.T) {
	var logOut bytes.Buffer
	cli.Log = &logrus.Logger{
		Out:       &logOut,
		Formatter: new(logrus.JSONFormatter),
		Level:     logrus.DebugLevel,
	}
	flags := 42

	file := newLoggingFile(nodefs.NewDataFile([]byte{}), "")
	code := file.Fsync(flags)
	assert.False(t, code.Ok())

	var output map[string]interface{}
	assert.NoError(t, json.Unmarshal(logOut.Bytes(), &output))

	assert.NotEmpty(t, output["status"])
	assert.Equal(t, "File Fsync", output["msg"])
	assert.True(t, output["duration_ms"].(float64) >= 0)
	assert.Equal(t, "[42]", output["args"])
	assert.NotEmpty(t, output["time"])
}

func TestIsDirectory_Directory(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "test")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	fooDir := path.Join(tempDir, "foo")

	err = os.Mkdir(fooDir, 0777)
	if err != nil {
		log.Fatal(err)
	}

	isDirectory, err := IsDirectory(fooDir)
	if err != nil {
		log.Fatal(err)
	}

	assert.True(t, isDirectory)
}

func TestIsDirectory_DirectoryWithDifferentCase(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "test")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	fooDir := path.Join(tempDir, "foo")

	err = os.Mkdir(fooDir, 0777)
	if err != nil {
		log.Fatal(err)
	}

	barDir := path.Join(tempDir, "Foo")

	isDirectory, err := IsDirectory(barDir)
	if err != nil {
		log.Fatal(err)
	}

	assert.False(t, isDirectory)
}

func TestIsDirectory_File(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "test")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	fooFile := path.Join(tempDir, "foo")

	file, err := os.Create(fooFile)
	if err != nil {
		log.Fatal(err)
	}
	file.Close()

	isDirectory, err := IsDirectory(fooFile)
	if err != nil {
		log.Fatal(err)
	}

	assert.False(t, isDirectory)
}

