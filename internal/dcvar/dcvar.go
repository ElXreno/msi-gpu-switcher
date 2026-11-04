package dcvar

import "fmt"

const (
	Name = "MsiDCVarData"
	GUID = "DD96BAAF-145E-4F56-B1CF-193256298E99"

	DefaultPath = "/sys/firmware/efi/efivars/" + Name + "-" + GUID
)

const GPUByte = 5

const (
	CodeHybrid     byte = 0
	CodeDiscrete   byte = 1
	CodeIntegrated byte = 2
)

const (
	gpuRequestMask      = 0x03
	gpuAppliedMask      = 0x0c
	gpuAppliedShift     = 2
	gpuThreeMode        = 0x10
	gpuIntegratedSwitch = 0x20
	gpuNoDiscrete       = 0x40
)

type GPU byte

func ParseGPU(data []byte) (GPU, error) {
	if len(data) <= GPUByte {
		return 0, fmt.Errorf("%s too small for the GPU field: %d bytes", Name, len(data))
	}
	return GPU(data[GPUByte]), nil
}

func (g GPU) ThreeMode() bool {
	return g&gpuThreeMode != 0
}

func (g GPU) IntegratedSupported() bool {
	return g&gpuIntegratedSwitch != 0
}

func (g GPU) DiscreteSupported() bool {
	return g&gpuNoDiscrete == 0
}

func (g GPU) Applied() byte {
	return byte(g&gpuAppliedMask) >> gpuAppliedShift
}

func (g GPU) Requested() byte {
	return byte(g & gpuRequestMask)
}

func (g GPU) WithRequest(code byte) GPU {
	return g&^gpuRequestMask | GPU(code&gpuRequestMask)
}
