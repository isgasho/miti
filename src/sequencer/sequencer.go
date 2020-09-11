package sequencer

import (
	"fmt"
	"io/ioutil"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/schollz/miti/src/click"
	"github.com/schollz/miti/src/log"
	"github.com/schollz/miti/src/metronome"
	"github.com/schollz/miti/src/music"
)

const QUARTERNOTES_PER_MEASURE = 4

type Sequencer struct {
	metronome *metronome.Metronome
	Sections  []Section

	chain     []string
	chainID   map[string]int
	sectionID int

	measure, section int
	midiPlay         func(string, music.Chord)
	latency          int64
	clickTrack       bool
	sync.Mutex
}

type Section struct {
	Name        string
	Parts       []Part
	NumMeasures int
	Tempo       float64
}

// Part contains the list of instruments and their measures
type Part struct {
	Instruments []string
	Measures    []Measure
	Legato      int
}

// Measure is all the notes contained within 4-beats
type Measure struct {
	// Emit contains the data that will be emitted
	Emit   map[int][]music.Chord
	Chords []music.Chord
}

func New(clickTrack bool, latency int64, midiPlay func(string, music.Chord)) (s *Sequencer) {
	s = new(Sequencer)
	s.metronome = metronome.New(s.Emit)
	s.midiPlay = midiPlay
	s.latency = latency
	s.clickTrack = clickTrack
	s.chainID = make(map[string]int)
	return
}

func (s *Sequencer) Start() {
	s.measure = -1
	s.section = 0
	if s.clickTrack {
		click.Play(60)
	}
	if len(s.Sections) > 0 {
		s.UpdateTempo(s.Sections[s.chainID[s.chain[s.section]]].Tempo)
	}
	s.metronome.Start()
}

func (s *Sequencer) Stop() {
	s.metronome.Stop()
}

func (s *Sequencer) UpdateTempo(tempo float64) {
	s.metronome.UpdateTempo(tempo)
	if s.clickTrack {
		click.SetBPM(tempo)
	}
}

func (s *Sequencer) Emit(pulse int) {
	s.Lock()
	defer s.Unlock()
	if len(s.Sections) == 0 {
		return
	}

	if pulse == 0 {
		s.measure++
		if s.measure == s.Sections[s.sectionID].NumMeasures {
			s.section++
			s.section = s.section % len(s.chain)
			s.sectionID = s.chainID[s.chain[s.section]]
			s.measure = 0

			// update tempo for new section
			if s.Sections[s.sectionID].Tempo != 0 {
				s.UpdateTempo(s.Sections[s.sectionID].Tempo)
			}
		}
		log.Trace(s.section, s.measure, pulse)
	}
	if s.clickTrack && math.Mod(float64(pulse), metronome.PULSES_PER_QUARTER_NOTE) == 0 {
		log.Trace("should click!")
		click.Click(s.latency)
	}

	// check for notes to emit
	for _, part := range s.Sections[s.sectionID].Parts {
		if len(part.Measures) == 0 {
			continue
		}
		measure := part.Measures[s.measure%len(part.Measures)]
		if e, ok := measure.Emit[pulse]; ok {
			// emit
			log.Tracef("[%s] emit %+v", strings.Join(part.Instruments, ", "), e)
			for _, instrument := range part.Instruments {
				chordOff := music.Chord{On: false}
				chordOn := music.Chord{On: true}
				for _, chord := range e {
					if chord.On {
						chordOn.Notes = append(chordOn.Notes, chord.Notes...)
					} else {
						chordOff.Notes = append(chordOff.Notes, chord.Notes...)
					}
				}
				if len(chordOff.Notes) > 0 {
					//midi.Midi(instrument, chordOff)
					s.midiPlay(instrument, chordOff)
				}
				if len(chordOn.Notes) > 0 {
					//midi.Midi(instrument, chordOn)
					s.midiPlay(instrument, chordOn)
				}
			}
			log.Trace("finished emitting")
		}
	}
}

func (s *Sequencer) Parse(fname string) (err error) {
	startTime := time.Now()
	defer func() {
		log.Infof("parsed sequence '%s' in %2.1f ms", fname, 1000*time.Since(startTime).Seconds())
	}()

	b, err := ioutil.ReadFile(fname)
	if err != nil {
		return
	}
	data := string(b)

	newSections := []Section{}
	newChain := []string{}

	var section Section
	var part Part
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		log.Tracef("parsing %s", line)
		if strings.HasPrefix(line, "pattern") {
			if len(part.Instruments) > 0 {
				section.Parts = append(section.Parts, part)
			}
			if len(section.Parts) > 0 {
				maxMeasures := 0
				for _, part := range section.Parts {
					if len(part.Measures) > maxMeasures {
						maxMeasures = len(part.Measures)
					}
				}
				section.NumMeasures = maxMeasures
				newSections = append(newSections, section)
			}
			part = Part{}
			section = Section{Name: strings.TrimSpace(strings.TrimPrefix(line, "pattern")), Tempo: section.Tempo}
		} else if strings.HasPrefix(line, "#") {
			// this is a comment
			continue
		} else if strings.HasPrefix(line, "legato") {
			fs := strings.Fields(line)
			if len(fs) > 0 {
				part.Legato, err = strconv.Atoi(fs[1])
				if err != nil {
					err = fmt.Errorf("problem parsing legato: %s", fs[1])
					return
				}
				if part.Legato < 0 {
					part.Legato = 0
				}
				if part.Legato > 100 {
					part.Legato = 100
				}
			}
		} else if strings.HasPrefix(line, "chain") {
			fs := strings.Fields(line)
			if len(fs) > 1 {
				newChain = fs[1:]
			}
		} else if strings.HasPrefix(line, "tempo") {
			fs := strings.Fields(line)
			if len(fs) > 0 {
				section.Tempo, err = strconv.ParseFloat(fs[1], 64)
				if err != nil {
					err = fmt.Errorf("problem parsing tempo: %s", fs[1])
					return
				}
				if section.Tempo < 1 {
					section.Tempo = 1
				} else if section.Tempo > 300 {
					section.Tempo = 300
				}
			}
		} else if strings.HasPrefix(line, "instruments") {
			if len(part.Instruments) > 0 {
				section.Parts = append(section.Parts, part)
			}
			line = strings.TrimPrefix(line, "instruments")
			line = strings.TrimPrefix(line, "instrument")
			instruments := strings.Split(line, ",")
			for i := range instruments {
				instruments[i] = strings.TrimSpace(instruments[i])
			}
			part = Part{Instruments: instruments, Legato: 100}
		} else if len(line) > 0 {
			measure := Measure{Emit: make(map[int][]music.Chord)}
			fs := strings.Fields(line)
			for i, cluster := range fs {
				if cluster == "." {
					continue
				}
				if strings.HasPrefix(cluster, ":") {
					// interpret as a chord
					log.Tracef("parsing '%s'", cluster)
					cluster = strings.TrimPrefix(cluster, ":")
					extra := strings.Split(cluster, ":")
					var notes []music.Note
					log.Tracef("converting chord '%s' to notes", extra[0])
					notes, err = music.ChordToNotes(extra[0])
					if err != nil {
						return
					}
					cluster = ""
					octaveAdj := 0
					holdChord := false
					if len(extra) > 1 {
						if strings.HasSuffix(extra[1], "-") {
							holdChord = true
							extra[1] = strings.TrimSuffix(extra[1], "-")
						}
						startOctave, errR := strconv.Atoi(extra[1])
						if errR == nil {
							octaveAdj = startOctave - notes[0].Octave
						}
					}
					for _, note := range notes {
						cluster += fmt.Sprintf("%s%d", note.Name, note.Octave+octaveAdj)
					}
					if holdChord {
						cluster += "-"
					}
				}
				holdNote := false
				if strings.HasSuffix(cluster, "-") {
					holdNote = true
					cluster = strings.TrimSuffix(cluster, "-")
				}
				var notes []music.Note
				if len(measure.Chords) > 0 {
					lastChord := measure.Chords[len(measure.Chords)-1]
					lastNote := lastChord.Notes[len(lastChord.Notes)-1]
					notes, err = music.ParseCluster(cluster, lastNote)
				} else {
					notes, err = music.ParseCluster(cluster)
				}
				if err != nil {
					log.Error(err)
					return
				}
				measure.Chords = append(measure.Chords, music.Chord{Notes: notes})
				startPulse := float64(i) / float64(len(fs)) * (QUARTERNOTES_PER_MEASURE*metronome.PULSES_PER_QUARTER_NOTE - 1)
				endPulse := startPulse + float64(part.Legato)/100.0*1/float64(len(fs))*(QUARTERNOTES_PER_MEASURE*metronome.PULSES_PER_QUARTER_NOTE-1)
				startPulse = math.Round(startPulse)
				endPulse = math.Round(endPulse)
				if startPulse < 0 {
					startPulse = 0
				} else if startPulse > (QUARTERNOTES_PER_MEASURE*metronome.PULSES_PER_QUARTER_NOTE - 2) {
					startPulse = (QUARTERNOTES_PER_MEASURE*metronome.PULSES_PER_QUARTER_NOTE - 2)
				}
				if endPulse < 1 {
					endPulse = 1
				} else if endPulse > (QUARTERNOTES_PER_MEASURE*metronome.PULSES_PER_QUARTER_NOTE - 1) {
					endPulse = (QUARTERNOTES_PER_MEASURE*metronome.PULSES_PER_QUARTER_NOTE - 1)
				}
				if endPulse <= startPulse {
					endPulse = startPulse + 1
				}

				if _, ok := measure.Emit[int(startPulse)]; !ok {
					measure.Emit[int(startPulse)] = []music.Chord{}
				}
				measure.Emit[int(startPulse)] = append(measure.Emit[int(startPulse)], music.Chord{Notes: notes, On: true})

				if !holdNote {
					if _, ok := measure.Emit[int(endPulse)]; !ok {
						measure.Emit[int(endPulse)] = []music.Chord{}
					}
					measure.Emit[int(endPulse)] = append(measure.Emit[int(endPulse)], music.Chord{Notes: notes, On: false})
				}
			}
			part.Measures = append(part.Measures, measure)
		}
	}
	if len(part.Instruments) > 0 {
		section.Parts = append(section.Parts, part)
	}
	if len(section.Parts) > 0 {
		maxMeasures := 0
		for _, part := range section.Parts {
			if len(part.Measures) > maxMeasures {
				maxMeasures = len(part.Measures)
			}
		}
		section.NumMeasures = maxMeasures
		newSections = append(newSections, section)
	}
	if len(newSections) > 0 {
		// validate chain / section names
		newChainMap := make(map[string]int)
		makeChain := len(newChain) == 0
		for i, section := range newSections {
			newChainMap[section.Name] = i
			if makeChain {
				newChain = append(newChain, section.Name)
			}
		}
		for _, chain := range newChain {
			if _, ok := newChainMap[chain]; !ok {
				err = fmt.Errorf("no such chain: %s", chain)
				return
			}
		}
		s.Lock()
		s.chainID = make(map[string]int)
		for k := range newChainMap {
			s.chainID[k] = newChainMap[k]
		}
		s.chain = newChain
		s.Sections = newSections
		s.Unlock()
	} else {
		err = fmt.Errorf("no sections found in data:\n----\n%s\n-----", data)
	}
	return
}
