package cards

// Example usage of EffectCard to replace specific effect cards:
//
// OLD WAY (specific cards, now removed):
//   flash := cards.NewFlashCard(100*time.Millisecond, subtle)
//   rainbow := cards.NewRainbowCard(0.5, 0.6)
//
// NEW WAY (unified EffectCard):
//   flash, _ := cards.NewEffectCard("flash", effects.EffectConfig{
//       "duration_ms":   100,
//       "color":         "#A0A0A0",
//       "max_intensity": 0.75,
//   })
//   rainbow, _ := cards.NewEffectCard("rainbow", effects.EffectConfig{
//       "speed_hz": 0.5,
//       "mix":      0.6,
//   })
//
// Pipeline usage (same for both):
//   pipe := cards.NewPipeline(nil,
//       cards.WrapApp(app),
//       flash,
//       rainbow,
//   )

