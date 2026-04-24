package entity_test

import (
	"github.com/bytter/autoresearch/internal/entity"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Goal markdown serialization", func() {
	It("round-trips current goal frontmatter and steering body", func() {
		flash := 65536.0
		ram := 16384.0
		g := &entity.Goal{
			SchemaVersion: entity.GoalSchemaVersion,
			Objective: entity.Objective{
				Instrument: "qemu_cycles",
				Target:     "dsp_fir_bench",
				Direction:  "decrease",
			},
			Completion: &entity.Completion{
				Threshold:   0.15,
				OnThreshold: entity.GoalOnThresholdAskHuman,
			},
			Constraints: []entity.Constraint{
				{Instrument: "size_flash", Max: &flash},
				{Instrument: "size_ram", Max: &ram},
				{Instrument: "host_test", Require: "pass"},
			},
			Body: "# Steering\n\nFocus on the hot inner loop.\n",
		}

		data, err := g.Marshal()
		Expect(err).NotTo(HaveOccurred())
		back, err := entity.ParseGoal(data)
		Expect(err).NotTo(HaveOccurred())

		Expect(back.Objective.Instrument).To(Equal("qemu_cycles"))
		Expect(back.Constraints).To(HaveLen(3))
		Expect(back.Constraints[0].Max).NotTo(BeNil())
		Expect(*back.Constraints[0].Max).To(Equal(65536.0))
		Expect(back.Constraints[2].Require).To(Equal("pass"))
		Expect(back.Completion).NotTo(BeNil())
		Expect(*back.Completion).To(Equal(entity.Completion{
			Threshold:   0.15,
			OnThreshold: entity.GoalOnThresholdAskHuman,
		}))
		Expect(back.Steering()).NotTo(BeEmpty())
	})

	It("maps legacy target_effect to ask-human completion", func() {
		data := []byte(`---
objective:
  instrument: qemu_cycles
  target: dsp_fir_bench
  direction: decrease
  target_effect: 0.15
constraints:
  - instrument: size_flash
    max: 65536
---

# Steering

focus on the hot inner loop
`)
		back, err := entity.ParseGoal(data)
		Expect(err).NotTo(HaveOccurred())
		Expect(back.Completion).NotTo(BeNil())
		Expect(*back.Completion).To(Equal(entity.Completion{
			Threshold:   0.15,
			OnThreshold: entity.GoalOnThresholdAskHuman,
		}))
	})

	It("rejects legacy target_effect when completion is also present", func() {
		data := []byte(`---
objective:
  instrument: qemu_cycles
  target: dsp_fir_bench
  direction: decrease
  target_effect: 0.15
completion:
  threshold: 0.2
  on_threshold: stop
constraints:
  - instrument: size_flash
    max: 65536
---

# Steering

focus on the hot inner loop
`)
		_, err := entity.ParseGoal(data)
		Expect(err).To(HaveOccurred())
	})

	It("round-trips rescuer clauses and the neutral band", func() {
		flash := 131072.0
		g := &entity.Goal{
			SchemaVersion: entity.GoalSchemaVersion,
			Objective: entity.Objective{
				Instrument: "ns_per_eval",
				Direction:  "decrease",
			},
			Constraints: []entity.Constraint{
				{Instrument: "size_flash", Max: &flash},
				{Instrument: "host_test", Require: "pass"},
			},
			Rescuers: []entity.Rescuer{
				{Instrument: "sim_total_bytes", Direction: "decrease", MinEffect: 0.02},
			},
			NeutralBandFrac: 0.02,
		}

		data, err := g.Marshal()
		Expect(err).NotTo(HaveOccurred())
		back, err := entity.ParseGoal(data)
		Expect(err).NotTo(HaveOccurred())

		Expect(back.Rescuers).To(Equal([]entity.Rescuer{
			{Instrument: "sim_total_bytes", Direction: "decrease", MinEffect: 0.02},
		}))
		Expect(back.NeutralBandFrac).To(Equal(0.02))
		Expect(back.SchemaVersion).To(Equal(entity.GoalSchemaVersion))
	})

	It("accepts schema v3 files written before rescuers existed", func() {
		// A goal on disk before rescuers were a concept. schema_version=3, no
		// rescuers or neutral_band_frac. It must parse cleanly without losing
		// any fields.
		data := []byte(`---
schema_version: 3
objective:
  instrument: ns_per_eval
  target: bench
  direction: decrease
constraints:
  - instrument: host_test
    require: pass
---

# Steering

focus on the hot inner loop
`)
		g, err := entity.ParseGoal(data)
		Expect(err).NotTo(HaveOccurred())
		Expect(g.Rescuers).To(BeEmpty())
		Expect(g.NeutralBandFrac).To(BeZero())
		Expect(g.SchemaVersion).To(Equal(3))
	})

	It("defaults missing completion policy to ask-human", func() {
		data := []byte(`---
objective:
  instrument: qemu_cycles
  target: dsp_fir_bench
  direction: decrease
completion:
  threshold: 0.15
constraints:
  - instrument: size_flash
    max: 65536
---

# Steering

focus on the hot inner loop
`)
		back, err := entity.ParseGoal(data)
		Expect(err).NotTo(HaveOccurred())
		Expect(back.Completion).NotTo(BeNil())
		Expect(back.Completion.OnThreshold).To(Equal(entity.GoalOnThresholdAskHuman))
	})
})
