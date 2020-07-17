package midi

import (
	"fmt"
	"strings"

	log "github.com/schollz/logger"
	"github.com/schollz/portmidi"
	"github.com/schollz/saps/src/music"
)

var outputStreams map[string]*portmidi.Stream
var encounteredNotes map[int64]struct{}
var inited bool

func Init() (devices []string, err error) {
	defer func() {
		if err == nil {
			inited = true
		}
	}()
	err = portmidi.Initialize()
	if err != nil {
		return
	}
	log.Debugf("found %d devices", portmidi.CountDevices())

	outputStreams = make(map[string]*portmidi.Stream)
	encounteredNotes = make(map[int64]struct{})
	for i := 0; i < portmidi.CountDevices(); i++ {
		di := portmidi.Info(portmidi.DeviceID(i))
		log.Debugf("device %d: '%s', i/o: %v/%v", i, di.Name, di.IsInputAvailable, di.IsOutputAvailable)
		if di.IsOutputAvailable && !strings.Contains(di.Name, "Microsoft") {
			devices = append(devices, di.Name)
			outputStreams[di.Name], err = portmidi.NewOutputStream(portmidi.DeviceID(i), 4096, 0)
			if err != nil {
				err = fmt.Errorf("could not get output from: '%s'", di.Name)
				return
			}
		}
	}
	return
}

func Shutdown() (err error) {
	inited = false
	err = NotesOff()
	if err != nil {
		log.Error(err)
	}
	for out := range outputStreams {
		outputStreams[out].Close()
	}
	return portmidi.Terminate()
}

func NotesOff() (err error) {
	for out := range outputStreams {
		for note := range encounteredNotes {
			log.Tracef("'%s' %d off ", out, note)
			err = outputStreams[out].WriteShort(0x80, note, 100)
		}
	}
	return
}

func Midi(msg string, chord music.Chord) (err error) {
	log.Trace("got emit")
	if !inited {
		err = fmt.Errorf("not initialized")
		return
	}
	if len(chord.Notes) == 0 {
		return
	}
	if _, ok := outputStreams[msg]; !ok {
		err = fmt.Errorf("no such device: %s", msg)
		return
	}
	log.Trace("building midi")
	midis := make([]int64, len(chord.Notes))
	velocities := make([]int64, len(chord.Notes))
	for i, n := range chord.Notes {
		midis[i] = int64(n.MIDI)
		encounteredNotes[midis[i]] = struct{}{}
		velocities[i] = 100
	}
	log.Trace("sending midi")
	if chord.On {
		log.Tracef("[%s] %+v", msg, midis)
		err = outputStreams[msg].WriteShorts(0x90, midis, velocities)
	} else {
		err = outputStreams[msg].WriteShorts(0x80, midis, velocities)
	}
	log.Trace("sent")
	if err != nil {
		return
	}
	return
}
