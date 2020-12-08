package watcherx

import (
	"context"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/fsnotify/fsnotify"
	"github.com/pkg/errors"
)

func WatchFile(ctx context.Context, file string, c EventChannel) (Watcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, errors.WithStack(err)
	}
	dir := filepath.Dir(file)
	if err := watcher.Add(dir); err != nil {
		return nil, errors.WithStack(err)
	}
	resolvedFile, err := filepath.EvalSymlinks(file)
	if err != nil {
		if _, ok := err.(*os.PathError); !ok {
			return nil, errors.WithStack(err)
		}
		// The file does not exist. The watcher should still watch the directory
		// to get notified about file creation.
		resolvedFile = ""
	} else if resolvedFile != file {
		// If `resolvedFile` != `file` then `file` is a symlink and we have to explicitly watch the referenced file.
		// This is because fsnotify follows symlinks and watches the destination file, not the symlink
		// itself. That is at least the case for unix systems. See: https://github.com/fsnotify/fsnotify/issues/199
		if err := watcher.Add(file); err != nil {
			return nil, errors.WithStack(err)
		}
	}
	d := newDispatcher()
	go streamFileEvents(ctx, watcher, c, d.trigger, file, resolvedFile)
	return d, nil
}

// streamFileEvents watches for file changes and supports symlinks which requires several workarounds due to limitations of fsnotify.
// Argument `resolvedFile` is the resolved symlink path of the file, or it is the watchedFile name itself. If `resolvedFile` is empty, then the watchedFile does not exist.
func streamFileEvents(ctx context.Context, watcher *fsnotify.Watcher, c EventChannel, sendNow <-chan struct{}, watchedFile, resolvedFile string) {
	eventSource := source(watchedFile)
	removeDirectFileWatcher := func() {
		_ = watcher.Remove(watchedFile)
	}
	addDirectFileWatcher := func() {
		// check if the watchedFile (symlink) exists
		// if it does not the dir watcher will notify us when it gets created
		if _, err := os.Lstat(watchedFile); err == nil {
			if err := watcher.Add(watchedFile); err != nil {
				c <- &ErrorEvent{
					error:  errors.WithStack(err),
					source: eventSource,
				}
			}
		}
	}
	for {
		select {
		case <-ctx.Done():
			_ = watcher.Close()
			close(c)
			return
		case <-sendNow:
			if resolvedFile == "" {
				// The file does not exist. Announce this by sending a RemoveEvent.
				c <- &RemoveEvent{eventSource}
			} else {
				// The file does exist. Announce the current content by sending a ChangeEvent.
				data, err := ioutil.ReadFile(watchedFile)
				if err != nil {
					c <- &ErrorEvent{
						error:  errors.WithStack(err),
						source: eventSource,
					}
					continue
				}
				c <- &ChangeEvent{
					data:   data,
					source: eventSource,
				}
			}
		case e, ok := <-watcher.Events:
			if !ok {
				close(c)
				return
			}
			// filter events to only watch watchedFile
			// e.Name contains the name of the watchedFile (regardless whether it is a symlink), not the resolved file name
			if filepath.Clean(e.Name) == watchedFile {
				recentlyResolvedFile, err := filepath.EvalSymlinks(watchedFile)
				// when there is no error the file exists and any symlinks can be resolved
				if err != nil {
					// check if the watchedFile (or the file behind the symlink) was removed
					if _, ok := err.(*os.PathError); ok {
						c <- &RemoveEvent{eventSource}
						removeDirectFileWatcher()
						continue
					}
					c <- &ErrorEvent{
						error:  errors.WithStack(err),
						source: eventSource,
					}
					continue
				}
				// This catches following three cases:
				// 1. the watchedFile was written or created
				// 2. the watchedFile is a symlink and has changed (k8s config map updates)
				// 3. the watchedFile behind the symlink was written or created
				switch {
				case recentlyResolvedFile != resolvedFile:
					resolvedFile = recentlyResolvedFile
					// watch the symlink again to update the actually watched file
					removeDirectFileWatcher()
					addDirectFileWatcher()
					// we fallthrough because we also want to read the file in this case
					fallthrough
				case e.Op&(fsnotify.Write|fsnotify.Create) != 0:
					data, err := ioutil.ReadFile(watchedFile)
					if err != nil {
						c <- &ErrorEvent{
							error:  errors.WithStack(err),
							source: eventSource,
						}
						continue
					}
					c <- &ChangeEvent{
						data:   data,
						source: eventSource,
					}
				}
			}
		}
	}
}
