package goadb

import (
	"log"
	"runtime"
	"strings"
	"sync/atomic"

	"github.com/zach-klippenstein/goadb/util"
	"github.com/zach-klippenstein/goadb/wire"
)

/*
DeviceWatcher publishes device status change events.
If the server dies while listening for events, it restarts the server.
*/
type DeviceWatcher struct {
	*deviceWatcherImpl
}

type DeviceStateChangedEvent struct {
	Serial   string
	OldState string
	NewState string
}

type deviceWatcherImpl struct {
	config ClientConfig

	// If an error occurs, it is stored here and eventChan is close immediately after.
	err atomic.Value

	eventChan chan DeviceStateChangedEvent

	// Function to start the server if it's not running or dies.
	startServer func() error
}

func NewDeviceWatcher(config ClientConfig) (*DeviceWatcher, error) {
	watcher := &DeviceWatcher{&deviceWatcherImpl{
		config:      config.sanitized(),
		eventChan:   make(chan DeviceStateChangedEvent),
		startServer: StartServer,
	}}

	runtime.SetFinalizer(watcher, func(watcher *DeviceWatcher) {
		watcher.Shutdown()
	})

	go publishDevices(watcher.deviceWatcherImpl)

	return watcher, nil
}

/*
C returns a channel than can be received on to get events.
If an unrecoverable error occurs, or Shutdown is called, the channel will be closed.
*/
func (w *DeviceWatcher) C() <-chan DeviceStateChangedEvent {
	return w.eventChan
}

// Err returns the error that caused the channel returned by C to be closed, if C is closed.
// If C is not closed, its return value is undefined.
func (w *DeviceWatcher) Err() error {
	if err, ok := w.err.Load().(error); ok {
		return err
	}
	return nil
}

// Shutdown stops the watcher from listening for events and closes the channel returned
// from C.
func (w *DeviceWatcher) Shutdown() {
	// TODO(z): Implement.
}

func (w *deviceWatcherImpl) reportErr(err error) {
	w.err.Store(err)
}

/*
publishDevices reads device lists from scanner, calculates diffs, and publishes events on
eventChan.
Returns when scanner returns an error.
Doesn't refer directly to a *DeviceWatcher so it can be GCed (which will,
in turn, close Scanner and stop this goroutine).

TODO: to support shutdown, spawn a new goroutine each time a server connection is established.
This goroutine should read messages and send them to a message channel. Can write errors directly
to errVal. publisHDevicesUntilError should take the msg chan and the scanner and select on the msg chan and stop chan, and if the stop
chan sends, close the scanner and return true. If the msg chan closes, just return false.
publishDevices can look at ret val: if false and err == EOF, reconnect. If false and other error, report err
and abort. If true, report no error and stop.
*/
func publishDevices(watcher *deviceWatcherImpl) {
	defer close(watcher.eventChan)

	var lastKnownStates map[string]string
	finished := false

	for {
		scanner, err := connectToTrackDevices(watcher.config.Dialer)
		if err != nil {
			watcher.reportErr(err)
			return
		}

		finished, err = publishDevicesUntilError(scanner, watcher.eventChan, &lastKnownStates)

		if finished {
			scanner.Close()
			return
		}

		if util.HasErrCode(err, util.ConnectionResetError) {
			// The server died, restart and reconnect.
			log.Println("[DeviceWatcher] server died, restarting…")
			if err := watcher.startServer(); err != nil {
				log.Println("[DeviceWatcher] error restarting server, giving up")
				watcher.reportErr(err)
				return
			} // Else server should be running, continue listening.
		} else {
			// Unknown error, don't retry.
			watcher.reportErr(err)
			return
		}
	}
}

func connectToTrackDevices(dialer Dialer) (wire.Scanner, error) {
	conn, err := dialer.Dial()
	if err != nil {
		return nil, err
	}

	if err := wire.SendMessageString(conn, "host:track-devices"); err != nil {
		conn.Close()
		return nil, err
	}

	if err := wire.ReadStatusFailureAsError(conn, "host:track-devices"); err != nil {
		conn.Close()
		return nil, err
	}

	return conn, nil
}

func publishDevicesUntilError(scanner wire.Scanner, eventChan chan<- DeviceStateChangedEvent, lastKnownStates *map[string]string) (finished bool, err error) {
	for {
		msg, err := scanner.ReadMessage()
		if err != nil {
			return false, err
		}

		deviceStates, err := parseDeviceStates(string(msg))
		if err != nil {
			return false, err
		}

		for _, event := range calculateStateDiffs(*lastKnownStates, deviceStates) {
			eventChan <- event
		}
		*lastKnownStates = deviceStates
	}
}

func parseDeviceStates(msg string) (states map[string]string, err error) {
	states = make(map[string]string)

	for lineNum, line := range strings.Split(msg, "\n") {
		if len(line) == 0 {
			continue
		}

		fields := strings.Split(line, "\t")
		if len(fields) != 2 {
			err = util.Errorf(util.ParseError, "invalid device state line %d: %s", lineNum, line)
			return
		}

		serial, state := fields[0], fields[1]
		states[serial] = state
	}

	return
}

func calculateStateDiffs(oldStates, newStates map[string]string) (events []DeviceStateChangedEvent) {
	for serial, oldState := range oldStates {
		newState, ok := newStates[serial]

		if oldState != newState {
			if ok {
				// Device present in both lists: state changed.
				events = append(events, DeviceStateChangedEvent{serial, oldState, newState})
			} else {
				// Device only present in old list: device removed.
				events = append(events, DeviceStateChangedEvent{serial, oldState, ""})
			}
		}
	}

	for serial, newState := range newStates {
		if _, ok := oldStates[serial]; !ok {
			// Device only present in new list: device added.
			events = append(events, DeviceStateChangedEvent{serial, "", newState})
		}
	}

	return events
}
