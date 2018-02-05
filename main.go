/*
MIT License

Copyright (c) 2018 Ken Haines

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
 */

package main

import (
	log "github.com/sirupsen/logrus"
	"flag"
	"github.com/go-fsnotify/fsnotify"
	"os"

	"time"

	"os/signal"
	"syscall"
	"io/ioutil"
	"path"
	"net/http"
)

var (
	watchedPath      = flag.String("watch-path", "/config", "Path to be watched (default /config)")
	expandVars       = flag.Bool("expand-vars", true, "Expand $env variables found in files (default true).")
	copyFiles        = flag.Bool("copy-files", true, "Copy config files to destination (default true)")
	targetPath       = flag.String("target-path", "/processed-config", "Path to copy processed files to (default /processed-config)")
	prometheusUrl    = flag.String("prometheus-url", "http://localhost:9090/-/reload", "Url to send a POST to prometheus for it to reload its config. (default http://localhost:9090/-/reload)")
	processDelayTime = flag.Duration("process-delay-time", 5*time.Second, "time to wait after a detected change to process files. This allows capturing multiple close timed changes in a single update.")
	debugLogs        = flag.Bool("debug", false, "Enable debug log output")
)

func main() {
	log.SetLevel(log.InfoLevel)
	log.Info("Prometheus Configuration Watcher")
	log.Info("Github: https://github.com/khaines/prom-config-watcher")
	flag.Parse()

	sigs := make(chan os.Signal, 1)

	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	lastConfigProcess := time.Time{}
	// initializing config change to now will trigger an initial run to process the config files
	lastConfigChange := time.Now()
	delayTimer := time.NewTimer(0)

	fileChangeTime, err := startWatchingPath(*watchedPath)
	if err != nil {
		log.Fatalf("Failed to start watching path %v, exiting")
		os.Exit(1)
		return
	}

	for {
		select {
		case <-sigs:
			log.Infof("Received SIGINT or SIGTERM. Shutting down")
			os.Exit(0)
			return
		case lastConfigChange = <-fileChangeTime:
			// reset the delay timer in case other changes are triggered rapidly
			delayTimer.Reset(*processDelayTime)

		case <-delayTimer.C:
			// process delay timer has tripped, process the config files.
			if lastConfigProcess.Before(lastConfigChange) {
				// process
				processConfigChanges(*watchedPath, *targetPath)
				notifyPrometheus(*prometheusUrl)
				lastConfigProcess = time.Now()
			}

		}

	}

}

func notifyPrometheus(url string) {
	log.Debug("Posting reload command to Prometheus")
	resp, err := http.Post(url, "plain/text", nil)
	if err != nil {
		log.Errorf("Error posting reload command to Prometheues: %v", err)
		return
	}
	defer resp.Body.Close()
	log.Debugf("Status code %v", resp.StatusCode)
}

func processConfigChanges(srcPath string, dstPath string) {
	log.Debugf("Processing changes for %v", srcPath)
	// if we are in a folder, process the files within
	stat, err := os.Stat(srcPath)
	if err != nil {
		log.Errorf("Error processing changes in %v: %v", srcPath, err)
		return
	}

	if stat.IsDir() {
		files, err := ioutil.ReadDir(srcPath)
		if err != nil {
			log.Errorf("Failed to list files in %v: %v", srcPath, err)
			return
		}

		for _, fileName := range files {
			processConfigChanges(path.Join(srcPath, fileName.Name()), dstPath)
		}
	} else {
		processFile(srcPath, dstPath)
	}
}

func processFile(filePath string, destFolder string) {

	// read in the file
	contents, err := ioutil.ReadFile(filePath)
	if err != nil {
		log.Errorf("Error reading %v: %v", filePath, err)
	}
	// expand any environmenal vars present
	updatedContent := os.ExpandEnv(string(contents))

	// write updated content to the destination folder
	_, fileName := path.Split(filePath)
	targetFile := path.Join(destFolder, fileName)
	log.Debugf("writing updated content to %v", targetFile)
	ioutil.WriteFile(targetFile, []byte(updatedContent), 0644)
}

func startWatchingPath(path string) (chan time.Time, error) {

	log.Debugf("Creating watcher for path %v", path)

	// need a channel for calling back about changes happening in files
	changeTime := make(chan time.Time)

	// create a file watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatalf("Error creating file watcher: %v", err)
		return nil, err
	}

	err = watcher.Add(path)
	if os.IsNotExist(err) {
		log.Infof("%v no longer exists", path)
	} else if err != nil {
		log.Warnf("Failed to watch %v because of error: %v", path, err)
	}

	// let the watcher run in the background
	go listenForChanges(watcher, changeTime)

	return changeTime, nil
}

func listenForChanges(watcher *fsnotify.Watcher, changeTime chan time.Time) {

	// main loop for processing events from the FS watcher
	for {
		select {
		case err := <-watcher.Errors:
			log.Errorf("Error from watcher: %v", err)

		case event := <-watcher.Events:
			log.Debugf("Received an event for %v", event.Name)
			stat, err := os.Stat(event.Name)
			if err != nil {
				log.Errorf("Could not get modified time for %v: %v", event.Name, err)
				continue
			}
			log.Debugf("Modified time of %v is %v", event.Name, stat.ModTime())

			changeTime <- stat.ModTime()

		}
	}

}
