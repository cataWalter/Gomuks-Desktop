package tui

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gen2brain/malgo"
)

const (
	voiceSampleRate           = 48000
	maxVoiceRecordingDuration = 4 * time.Minute
)

type voiceRecorder struct {
	device       *malgo.Device
	closeContext func()
	mu           sync.Mutex
	pcm          []byte
}

type voiceRecordingLimitMsg struct{}

type voiceNoteSentMsg struct {
	err error
}

type audioInputDevice struct {
	id   string
	name string
}

type audioInputDevicesLoadedMsg struct {
	devices []audioInputDevice
	err     error
}

func audioInputDevicesCmd() tea.Cmd {
	return func() tea.Msg {
		devices, err := listAudioInputDevices()
		if err != nil {
			return audioInputDevicesLoadedMsg{err: err}
		}
		inputs := make([]audioInputDevice, 0, len(devices))
		for _, device := range devices {
			inputs = append(inputs, audioInputDevice{id: device.ID.String(), name: device.Name()})
		}
		return audioInputDevicesLoadedMsg{devices: inputs}
	}
}

func listAudioInputDevices() ([]malgo.DeviceInfo, error) {
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = ctx.Uninit()
		ctx.Free()
	}()
	devices, err := ctx.Devices(malgo.Capture)
	if err != nil {
		return nil, err
	}
	return devices, nil
}

func startVoiceRecording(deviceID string) (*voiceRecorder, error) {
	var selectedDeviceID malgo.DeviceID
	if deviceID != "" {
		decoded, decodeErr := hex.DecodeString(deviceID)
		if decodeErr != nil || len(decoded) > len(selectedDeviceID) {
			return nil, fmt.Errorf("invalid microphone device ID %q", deviceID)
		}
		copy(selectedDeviceID[:], decoded)
	}
	audioContext, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return nil, fmt.Errorf("initialize microphone: %w", err)
	}
	recorder := &voiceRecorder{closeContext: func() {
		_ = audioContext.Uninit()
		audioContext.Free()
	}}
	deviceConfig := malgo.DefaultDeviceConfig(malgo.Capture)
	deviceConfig.Capture.Format = malgo.FormatS16
	deviceConfig.Capture.Channels = 1
	deviceConfig.SampleRate = voiceSampleRate
	if deviceID != "" {
		deviceConfig.Capture.DeviceID = selectedDeviceID.Pointer()
	}
	device, err := malgo.InitDevice(audioContext.Context, deviceConfig, malgo.DeviceCallbacks{
		Data: func(_, input []byte, _ uint32) {
			recorder.mu.Lock()
			defer recorder.mu.Unlock()
			maxBytes := voiceSampleRate * 2 * int(maxVoiceRecordingDuration/time.Second)
			remaining := maxBytes - len(recorder.pcm)
			if remaining > 0 {
				recorder.pcm = append(recorder.pcm, input[:min(len(input), remaining)]...)
			}
		},
	})
	if err != nil {
		recorder.Close()
		return nil, fmt.Errorf("initialize microphone: %w", err)
	}
	recorder.device = device
	if err = device.Start(); err != nil {
		recorder.Close()
		return nil, fmt.Errorf("start microphone: %w", err)
	}
	return recorder, nil
}

func (r *voiceRecorder) Stop() ([]byte, int64, error) {
	r.Close()
	r.mu.Lock()
	pcm := append([]byte(nil), r.pcm...)
	r.pcm = nil
	r.mu.Unlock()
	if len(pcm) == 0 {
		return nil, 0, errors.New("no audio was captured")
	}
	durationMilliseconds := int64(len(pcm)) * 1000 / int64(voiceSampleRate*2)
	data, err := encodeVoiceWAV(pcm, voiceSampleRate)
	return data, durationMilliseconds, err
}

func (r *voiceRecorder) Close() {
	if r.device != nil {
		_ = r.device.Stop()
		r.device.Uninit()
		r.device = nil
	}
	if r.closeContext != nil {
		r.closeContext()
		r.closeContext = nil
	}
}

func encodeVoiceWAV(pcm []byte, sampleRate int) ([]byte, error) {
	if sampleRate <= 0 || len(pcm) == 0 || len(pcm)%2 != 0 {
		return nil, errors.New("invalid PCM audio data")
	}
	if uint64(len(pcm))+36 > uint64(^uint32(0)) {
		return nil, errors.New("voice note is too large to encode")
	}
	wav := make([]byte, 44+len(pcm))
	copy(wav[0:4], "RIFF")
	binary.LittleEndian.PutUint32(wav[4:8], uint32(len(wav)-8))
	copy(wav[8:12], "WAVE")
	copy(wav[12:16], "fmt ")
	binary.LittleEndian.PutUint32(wav[16:20], 16)
	binary.LittleEndian.PutUint16(wav[20:22], 1)
	binary.LittleEndian.PutUint16(wav[22:24], 1)
	binary.LittleEndian.PutUint32(wav[24:28], uint32(sampleRate))
	binary.LittleEndian.PutUint32(wav[28:32], uint32(sampleRate*2))
	binary.LittleEndian.PutUint16(wav[32:34], 2)
	binary.LittleEndian.PutUint16(wav[34:36], 16)
	copy(wav[36:40], "data")
	binary.LittleEndian.PutUint32(wav[40:44], uint32(len(pcm)))
	copy(wav[44:], pcm)
	return wav, nil
}
