package main

import (
    "encoding/binary"
    "encoding/hex"
    "flag"
    "fmt"
    "io"
    "math"
    "math/cmplx"
    "os"
)

const (
    sampleRate = 44100
    duration   = 0.2
    fftSize    = 8820
    amplitude8Bit  = 127
    amplitude16Bit = 32760
    baseFreq    = 440.0
    freqStep    = 25.0    // Larger step size since we only need 16 values
)

var (
    toneBuffers8Bit  [16][]byte
    toneBuffers16Bit [16][]byte
)

func init() {
    for i := 0; i < 16; i++ {
        freq := baseFreq + float64(i)*freqStep
        toneBuffers8Bit[i] = generateToneBuffer(freq, false)
        toneBuffers16Bit[i] = generateToneBuffer(freq, true)
    }
}

func main() {
    decode := flag.Bool("d", false, "decode mode")
    use16bit := flag.Bool("16", false, "use 16-bit audio (default: 8-bit)")
    flag.Parse()

    input, err := io.ReadAll(os.Stdin)
    if err != nil {
        fmt.Fprintf(os.Stderr, "Error reading input: %v\n", err)
        os.Exit(1)
    }

    if *decode {
        decodeTones(input, *use16bit)
    } else {
        encodeTones(input, *use16bit)
    }
}

func generateToneBuffer(freq float64, use16bit bool) []byte {
    numSamples := int(sampleRate * duration)
    amplitude := amplitude8Bit
    bytesPerSample := 1
    
    if use16bit {
        amplitude = amplitude16Bit
        bytesPerSample = 2
    }
    
    buf := make([]byte, numSamples*bytesPerSample)
    
    for i := 0; i < numSamples; i++ {
        t := float64(i) / float64(sampleRate)
        window := 0.5 * (1 - math.Cos(2*math.Pi*float64(i)/float64(numSamples-1)))
        sample := int(float64(amplitude) * math.Sin(2*math.Pi*freq*t) * window)
        
        if use16bit {
            binary.BigEndian.PutUint16(buf[i*2:], uint16(sample))
        } else {
            buf[i] = uint8(sample + amplitude8Bit)
        }
    }
    return buf
}

func encodeTones(data []byte, use16bit bool) {
    writeAuHeader(use16bit)
    
    buffers := toneBuffers8Bit
    if use16bit {
        buffers = toneBuffers16Bit
    }
    
    hexStr := hex.EncodeToString(data)
    for _, c := range hexStr {
        var index int
        switch {
        case c >= '0' && c <= '9':
            index = int(c - '0')
        case c >= 'a' && c <= 'f':
            index = int(c - 'a' + 10)
        case c >= 'A' && c <= 'F':
            index = int(c - 'A' + 10)
        }
        os.Stdout.Write(buffers[index])
    }
}

func writeAuHeader(use16bit bool) {
    encoding := byte(0x02)
    if use16bit {
        encoding = byte(0x03)
    }
    
    header := []byte{
        0x2e, 0x73, 0x6e, 0x64,
        0x00, 0x00, 0x00, 0x20,
        0xff, 0xff, 0xff, 0xff,
        0x00, 0x00, 0x00, encoding,
        0x00, 0x00, 0xac, 0x44,
        0x00, 0x00, 0x00, 0x01,
        0x00, 0x00, 0x00, 0x00,
        0x00, 0x00, 0x00, 0x00,
    }
    os.Stdout.Write(header)
}

func decodeTones(input []byte, use16bit bool) {
    input = input[24:]
    
    bytesPerSample := 1
    if use16bit {
        bytesPerSample = 2
    }
    
    samples := make([]float64, len(input)/bytesPerSample)
    
    for i := 0; i < len(input); i += bytesPerSample {
        var sample float64
        if use16bit {
            sample = float64(int16(binary.BigEndian.Uint16(input[i:i+2]))) / amplitude16Bit
        } else {
            sample = (float64(input[i]) - amplitude8Bit) / amplitude8Bit
        }
        samples[i/bytesPerSample] = sample
    }

    hexOutput := make([]byte, 0)
    for i := 0; i < len(samples); i += fftSize {
        if i+fftSize > len(samples) {
            break
        }
        window := samples[i : i+fftSize]
        freq := detectFrequency(window)
        if freq > 0 {
            digit := freqToHex(freq)
            if digit != 255 {
                hexOutput = append(hexOutput, digit)
            }
        }
    }

    decoded := make([]byte, hex.DecodedLen(len(hexOutput)))
    _, err := hex.Decode(decoded, hexOutput)
    if err != nil {
        fmt.Fprintf(os.Stderr, "Error decoding hex: %v\n", err)
        os.Exit(1)
    }
    os.Stdout.Write(decoded)
}

func detectFrequency(samples []float64) float64 {
    input := make([]complex128, fftSize)
    
    for i := 0; i < len(samples); i++ {
        window := 0.5 * (1 - math.Cos(2*math.Pi*float64(i)/float64(len(samples)-1)))
        input[i] = complex(samples[i]*window, 0)
    }

    output := fft(input)

    maxMagnitude := 0.0
    maxIndex := 0
    
    for i := 1; i < fftSize/2-1; i++ {
        magnitude := cmplx.Abs(output[i])
        if magnitude > maxMagnitude &&
           magnitude > cmplx.Abs(output[i-1]) &&
           magnitude > cmplx.Abs(output[i+1]) {
            maxMagnitude = magnitude
            maxIndex = i
        }
    }

    if maxIndex > 0 && maxIndex < fftSize/2-1 {
        alpha := cmplx.Abs(output[maxIndex-1])
        beta := cmplx.Abs(output[maxIndex])
        gamma := cmplx.Abs(output[maxIndex+1])
        correction := 0.5 * (alpha - gamma) / (alpha - 2*beta + gamma)
        return (float64(maxIndex) + correction) * sampleRate / float64(fftSize)
    }

    return float64(maxIndex) * sampleRate / float64(fftSize)
}

func fft(input []complex128) []complex128 {
    n := len(input)
    if n == 1 {
        return input
    }

    even := make([]complex128, n/2)
    odd := make([]complex128, n/2)
    for i := 0; i < n/2; i++ {
        even[i] = input[2*i]
        odd[i] = input[2*i+1]
    }

    evenFFT := fft(even)
    oddFFT := fft(odd)

    result := make([]complex128, n)
    for k := 0; k < n/2; k++ {
        angle := -2 * math.Pi * float64(k) / float64(n)
        factor := cmplx.Rect(1, angle)
        result[k] = evenFFT[k] + factor*oddFFT[k]
        result[k+n/2] = evenFFT[k] - factor*oddFFT[k]
    }

    return result
}

func freqToHex(freq float64) byte {
    index := int(math.Round((freq - baseFreq) / freqStep))
    if index >= 0 && index < 16 {
        if index < 10 {
            return byte('0' + index)
        }
        return byte('a' + (index - 10))
    }
    return 255
}
