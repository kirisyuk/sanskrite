package propagation

// WaveDamping управляет затуханием волны распространения влияния
type WaveDamping struct {
	baseDampingFactor float64 // Базовый коэффициент затухания
}

// NewWaveDamping создает новый менеджер затухания волны
func NewWaveDamping() *WaveDamping {
	return &WaveDamping{
		baseDampingFactor: 0.8, // По умолчанию затухание на 20% на каждом шаге
	}
}

// ApplyDamping применяет затухание к изменению метрики
func (d *WaveDamping) ApplyDamping(delta float64, amplification float64, criticality float64) float64 {
	// Базовое затухание
	dampedDelta := delta * d.baseDampingFactor

	// Учитываем amplification (коэффициент усиления связи)
	dampedDelta = dampedDelta * amplification

	// Учитываем criticality (критичность связи)
	// Более критические связи меньше затухают
	criticalityFactor := 0.5 + (criticality * 0.5) // От 0.5 до 1.0
	dampedDelta = dampedDelta * criticalityFactor

	return dampedDelta
}

// SetBaseDampingFactor устанавливает базовый коэффициент затухания
func (d *WaveDamping) SetBaseDampingFactor(factor float64) {
	if factor < 0 {
		factor = 0
	}
	if factor > 1 {
		factor = 1
	}
	d.baseDampingFactor = factor
}

