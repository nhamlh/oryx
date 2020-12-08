package watcherx

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setup(t *testing.T) (context.Context, chan Event, string, context.CancelFunc) {
	c := make(chan Event)
	ctx, cancel := context.WithCancel(context.Background())
	dir, err := ioutil.TempDir("", "*")
	require.NoError(t, err)
	return ctx, c, dir, cancel
}

func assertChange(t *testing.T, e Event, expectedData, src string) {
	_, ok := e.(*ChangeEvent)
	require.True(t, ok, "%T: %+v", e, e)
	data, err := ioutil.ReadAll(e.Reader())
	require.NoError(t, err)
	assert.Equal(t, expectedData, string(data))
	assert.Equal(t, src, e.Source())
}

func assertRemove(t *testing.T, e Event, src string) {
	assert.Equal(t, &RemoveEvent{source(src)}, e)
}

func TestFileWatcher(t *testing.T) {
	t.Run("case=notifies on file write", func(t *testing.T) {
		ctx, c, dir, cancel := setup(t)
		defer cancel()

		exampleFile := filepath.Join(dir, "example.file")
		f, err := os.Create(exampleFile)
		require.NoError(t, err)

		_, err = WatchFile(ctx, exampleFile, c)
		require.NoError(t, err)

		_, err = fmt.Fprintf(f, "foo")
		require.NoError(t, err)
		require.NoError(t, f.Close())

		assertChange(t, <-c, "foo", exampleFile)
	})

	t.Run("case=notifies on file create", func(t *testing.T) {
		ctx, c, dir, cancel := setup(t)
		defer cancel()

		exampleFile := filepath.Join(dir, "example.file")
		_, err := WatchFile(ctx, exampleFile, c)
		require.NoError(t, err)

		f, err := os.Create(exampleFile)
		require.NoError(t, err)
		require.NoError(t, f.Close())

		assertChange(t, <-c, "", exampleFile)
	})

	t.Run("case=notifies after file delete about recreate", func(t *testing.T) {
		ctx, c, dir, cancel := setup(t)
		defer cancel()

		exampleFile := filepath.Join(dir, "example.file")
		f, err := os.Create(exampleFile)
		require.NoError(t, err)
		require.NoError(t, f.Close())

		_, err = WatchFile(ctx, exampleFile, c)
		require.NoError(t, err)

		require.NoError(t, os.Remove(exampleFile))

		assertRemove(t, <-c, exampleFile)

		f, err = os.Create(exampleFile)
		require.NoError(t, err)
		require.NoError(t, f.Close())

		assertChange(t, <-c, "", exampleFile)
	})

	t.Run("case=notifies about changes in the linked file", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("skipping test because watching symlinks on windows is not working properly")
		}

		ctx, c, dir, cancel := setup(t)
		defer cancel()

		otherDir, err := ioutil.TempDir("", "*")
		require.NoError(t, err)
		origFileName := filepath.Join(otherDir, "original")
		f, err := os.Create(origFileName)
		require.NoError(t, err)

		linkFileName := filepath.Join(dir, "slink")
		require.NoError(t, os.Symlink(origFileName, linkFileName))

		_, err = WatchFile(ctx, linkFileName, c)
		require.NoError(t, err)

		_, err = fmt.Fprintf(f, "content")
		require.NoError(t, err)
		require.NoError(t, f.Close())

		assertChange(t, <-c, "content", linkFileName)
	})

	t.Run("case=notifies about symlink change", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("skipping test because watching symlinks on windows is not working properly")
		}

		ctx, c, dir, cancel := setup(t)
		defer cancel()

		otherDir, err := ioutil.TempDir("", "*")
		require.NoError(t, err)
		fileOne := filepath.Join(otherDir, "fileOne")
		fileTwo := filepath.Join(otherDir, "fileTwo")
		f1, err := os.Create(fileOne)
		require.NoError(t, err)
		require.NoError(t, f1.Close())
		f2, err := os.Create(fileTwo)
		require.NoError(t, err)
		_, err = fmt.Fprintf(f2, "file two")
		require.NoError(t, err)
		require.NoError(t, f2.Close())

		linkFileName := filepath.Join(dir, "slink")
		require.NoError(t, os.Symlink(fileOne, linkFileName))

		_, err = WatchFile(ctx, linkFileName, c)
		require.NoError(t, err)

		require.NoError(t, os.Remove(linkFileName))
		assertRemove(t, <-c, linkFileName)

		require.NoError(t, os.Symlink(fileTwo, linkFileName))
		assertChange(t, <-c, "file two", linkFileName)
	})

	t.Run("case=watch relative file path", func(t *testing.T) {
		ctx, c, dir, cancel := setup(t)
		defer cancel()

		require.NoError(t, os.Chdir(dir))

		fileName := "example.file"
		_, err := WatchFile(ctx, fileName, c)
		require.NoError(t, err)

		f, err := os.Create(fileName)
		require.NoError(t, err)
		require.NoError(t, f.Close())

		assertChange(t, <-c, "", fileName)
	})

	// https://github.com/kubernetes/kubernetes/issues/93686
	//t.Run("case=kubernetes atomic writer create", func(t *testing.T) {
	//	ctx, c, dir, cancel := setup(t)
	//	defer cancel()
	//
	//	fileName := "example.file"
	//	filePath := path.Join(dir, fileName)
	//
	//	require.NoError(t, WatchFile(ctx, filePath, c))
	//
	//	kubernetesAtomicWrite(t, dir, fileName, "foobarx")
	//
	//	assertChange(t, <-c, "foobarx", filePath)
	//})

	t.Run("case=kubernetes atomic writer update", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("skipping test because watching symlinks on windows is not working properly")
		}

		ctx, c, dir, cancel := setup(t)
		defer cancel()

		fileName := "example.file"
		filePath := filepath.Join(dir, fileName)
		kubernetesAtomicWrite(t, dir, fileName, "foobar")

		_, err := WatchFile(ctx, filePath, c)
		require.NoError(t, err)

		kubernetesAtomicWrite(t, dir, fileName, "foobarx")

		assertChange(t, <-c, "foobarx", filePath)
	})

	t.Run("case=sends event when requested", func(t *testing.T) {
		ctx, c, dir, cancel := setup(t)
		defer cancel()

		fn := filepath.Join(dir, "example.file")
		initialContent := "initial content"
		require.NoError(t, ioutil.WriteFile(fn, []byte(initialContent), 0600))

		d, err := WatchFile(ctx, fn, c)
		require.NoError(t, err)
		require.NoError(t, d.DispatchNow())

		assertChange(t, <-c, initialContent, fn)
	})
}
