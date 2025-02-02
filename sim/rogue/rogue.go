package rogue

import (
	"time"

	"github.com/wowsims/wotlk/sim/core"
	"github.com/wowsims/wotlk/sim/core/proto"
	"github.com/wowsims/wotlk/sim/core/stats"
)

func RegisterRogue() {
	core.RegisterAgentFactory(
		proto.Player_Rogue{},
		proto.Spec_SpecRogue,
		func(character core.Character, options proto.Player) core.Agent {
			return NewRogue(character, options)
		},
		func(player *proto.Player, spec interface{}) {
			playerSpec, ok := spec.(*proto.Player_Rogue)
			if !ok {
				panic("Invalid spec value for Rogue!")
			}
			player.Spec = playerSpec
		},
	)
}

const (
	SpellFlagBuilder      = core.SpellFlagAgentReserved2
	SpellFlagFinisher     = core.SpellFlagAgentReserved3
	SpellFlagRogueAbility = SpellFlagBuilder | SpellFlagFinisher | core.SpellFlagAgentReserved1
)

type Rogue struct {
	core.Character

	Talents  proto.RogueTalents
	Options  proto.Rogue_Options
	Rotation proto.Rogue_Rotation

	// Current rotation plan.
	plan int

	// Cached values for calculating rotation.
	energyPerSecondAvg    float64
	eaBuildTime           time.Duration // Time to build EA following a finisher at ~35 energy
	sliceAndDiceDurations [6]time.Duration

	doneSND bool // Current SND will last for the rest of the iteration
	doneEA  bool // Current EA will last for the rest of the iteration, or not using EA

	disabledMCDs []*core.MajorCooldown

	// Assigned based on rotation, can be SS, Backstab, Hemo, etc
	Builder *core.Spell

	Backstab       *core.Spell
	DeadlyPoison   *core.Spell
	Hemorrhage     *core.Spell
	HungerForBlood *core.Spell
	InstantPoison  [3]*core.Spell
	Mutilate       *core.Spell
	Shiv           *core.Spell
	SinisterStrike *core.Spell

	Envenom      [6]*core.Spell
	Eviscerate   [6]*core.Spell
	ExposeArmor  *core.Spell
	Rupture      [6]*core.Spell
	SliceAndDice [6]*core.Spell

	LastDeadlyPoisonProcMask     core.ProcMask
	DeadlyPoisonProcChanceBonus  float64
	InstantPoisonProcChanceBonus float64
	DeadlyPoisonDots             []*core.Dot
	RuptureDot                   *core.Dot

	AdrenalineRushAura  *core.Aura
	BladeFlurryAura     *core.Aura
	DeathmantleProcAura *core.Aura
	EnvenomAura         *core.Aura
	ExposeArmorAura     *core.Aura
	HungerForBloodAura  *core.Aura
	KillingSpreeAura    *core.Aura
	OverkillAura        *core.Aura
	SliceAndDiceAura    *core.Aura

	QuickRecoveryMetrics *core.ResourceMetrics

	finishingMoveEffectApplier func(sim *core.Simulation, numPoints int32)
}

func (rogue *Rogue) GetCharacter() *core.Character {
	return &rogue.Character
}

func (rogue *Rogue) GetRogue() *Rogue {
	return rogue
}

func (rogue *Rogue) AddRaidBuffs(raidBuffs *proto.RaidBuffs)    {}
func (rogue *Rogue) AddPartyBuffs(partyBuffs *proto.PartyBuffs) {}

func (rogue *Rogue) finisherFlags() core.SpellFlag {
	flags := SpellFlagFinisher
	if rogue.Talents.SurpriseAttacks {
		flags |= core.SpellFlagCannotBeDodged
	}
	return flags
}

func (rogue *Rogue) ApplyFinisher(sim *core.Simulation, spell *core.Spell) {
	numPoints := rogue.ComboPoints()
	rogue.SpendComboPoints(sim, spell.ComboPointMetrics())
	rogue.finishingMoveEffectApplier(sim, numPoints)
}

func (rogue *Rogue) HasMajorGlyph(glyph proto.RogueMajorGlyph) bool {
	return rogue.HasGlyph(int32(glyph))
}

func (rogue *Rogue) HasMinorGlyph(glyph proto.RogueMinorGlyph) bool {
	return rogue.HasGlyph(int32(glyph))
}

func (rogue *Rogue) Initialize() {
	// Update auto crit multipliers now that we have the targets.
	rogue.AutoAttacks.MHEffect.OutcomeApplier = rogue.OutcomeFuncMeleeWhite(rogue.MeleeCritMultiplier(true, false))
	rogue.AutoAttacks.OHEffect.OutcomeApplier = rogue.OutcomeFuncMeleeWhite(rogue.MeleeCritMultiplier(false, false))

	if rogue.Talents.QuickRecovery > 0 {
		rogue.QuickRecoveryMetrics = rogue.NewEnergyMetrics(core.ActionID{SpellID: 31245})
	}

	rogue.registerBackstabSpell()
	rogue.registerDeadlyPoisonSpell()
	rogue.registerEviscerate()
	rogue.registerExposeArmorSpell()
	rogue.registerHemorrhageSpell()
	rogue.registerInstantPoisonSpell()
	rogue.registerMutilateSpell()
	rogue.registerRupture()
	rogue.registerShivSpell()
	rogue.registerSinisterStrikeSpell()
	rogue.registerSliceAndDice()

	rogue.registerThistleTeaCD()

	if rogue.Rotation.UseEnvenom {
		rogue.registerEnvenom()
	}

	switch rogue.Rotation.Builder {
	case proto.Rogue_Rotation_SinisterStrike:
		rogue.Builder = rogue.SinisterStrike
	case proto.Rogue_Rotation_Backstab:
		rogue.Builder = rogue.Backstab
	case proto.Rogue_Rotation_Hemorrhage:
		rogue.Builder = rogue.Hemorrhage
	case proto.Rogue_Rotation_Mutilate:
		rogue.Builder = rogue.Mutilate
	}

	rogue.finishingMoveEffectApplier = rogue.makeFinishingMoveEffectApplier()

	rogue.energyPerSecondAvg = (core.EnergyPerTick*rogue.EnergyTickMultiplier)/core.EnergyTickDuration.Seconds() + 5.0

	// TODO: Currently assumes default combat spec.
	expectedComboPointsAfterFinisher := 0
	expectedEnergyAfterFinisher := 25.0
	comboPointsNeeded := 5 - expectedComboPointsAfterFinisher
	energyForEA := rogue.Builder.DefaultCast.Cost*float64(comboPointsNeeded) + rogue.ExposeArmor.DefaultCast.Cost
	rogue.eaBuildTime = time.Duration(((energyForEA - expectedEnergyAfterFinisher) / rogue.energyPerSecondAvg) * float64(time.Second))

	rogue.DelayDPSCooldownsForArmorDebuffs()
}

func (rogue *Rogue) Reset(sim *core.Simulation) {
	rogue.plan = PlanOpener
	rogue.doneSND = false

	permaEA := rogue.ExposeArmorAura.Duration == core.NeverExpires
	rogue.doneEA = !rogue.Rotation.MaintainExposeArmor || permaEA

	rogue.disabledMCDs = rogue.DisableAllEnabledCooldowns(core.CooldownTypeUnknown)
}

func (rogue *Rogue) MeleeCritMultiplier(isMH bool, applyLethality bool) float64 {
	primaryModifier := rogue.murderMultiplier()
	secondaryModifier := 0.0
	preyModifier := rogue.preyOnTheWeakMultiplier(rogue.CurrentTarget)
	if applyLethality {
		secondaryModifier += 0.06 * float64(rogue.Talents.Lethality)
	}
	primaryModifier *= preyModifier
	return rogue.Character.MeleeCritMultiplier(primaryModifier, secondaryModifier)
}
func (rogue *Rogue) SpellCritMultiplier() float64 {
	primaryModifier := rogue.preyOnTheWeakMultiplier(rogue.CurrentTarget)
	primaryModifier *= rogue.murderMultiplier()
	return rogue.Character.SpellCritMultiplier(primaryModifier, 0)
}

func NewRogue(character core.Character, options proto.Player) *Rogue {
	rogueOptions := options.GetRogue()

	rogue := &Rogue{
		Character: character,
		Talents:   *rogueOptions.Talents,
		Options:   *rogueOptions.Options,
		Rotation:  *rogueOptions.Rotation,
	}
	rogue.applyPoisons()

	// Passive rogue threat reduction: https://wotlk.wowhead.com/spell=21184/rogue-passive-dnd
	rogue.PseudoStats.ThreatMultiplier *= 0.71
	rogue.PseudoStats.CanParry = true

	daggerMH := rogue.Equip[proto.ItemSlot_ItemSlotMainHand].WeaponType == proto.WeaponType_WeaponTypeDagger
	daggerOH := rogue.Equip[proto.ItemSlot_ItemSlotOffHand].WeaponType == proto.WeaponType_WeaponTypeDagger
	dualDagger := daggerMH && daggerOH
	if rogue.Rotation.Builder == proto.Rogue_Rotation_Unknown {
		rogue.Rotation.Builder = proto.Rogue_Rotation_Auto
	}
	if rogue.Rotation.Builder == proto.Rogue_Rotation_Backstab && !daggerMH {
		rogue.Rotation.Builder = proto.Rogue_Rotation_Auto
	} else if rogue.Rotation.Builder == proto.Rogue_Rotation_Hemorrhage && !rogue.Talents.Hemorrhage {
		rogue.Rotation.Builder = proto.Rogue_Rotation_Auto
	} else if rogue.Rotation.Builder == proto.Rogue_Rotation_Mutilate && !rogue.Talents.Mutilate {
		rogue.Rotation.Builder = proto.Rogue_Rotation_Auto
	} else if rogue.Rotation.Builder == proto.Rogue_Rotation_Mutilate && !dualDagger {
		rogue.Rotation.Builder = proto.Rogue_Rotation_Auto
	}
	if rogue.Rotation.Builder == proto.Rogue_Rotation_Auto {
		if rogue.Talents.Mutilate && dualDagger {
			rogue.Rotation.Builder = proto.Rogue_Rotation_Mutilate
		} else if rogue.Talents.Hemorrhage {
			rogue.Rotation.Builder = proto.Rogue_Rotation_Hemorrhage
		} else if daggerMH {
			rogue.Rotation.Builder = proto.Rogue_Rotation_Backstab
		} else {
			rogue.Rotation.Builder = proto.Rogue_Rotation_SinisterStrike
		}
	}

	if rogue.Options.OhImbue != proto.Rogue_Options_DeadlyPoison {
		rogue.Rotation.UseShiv = false
	}

	maxEnergy := 100.0
	if rogue.Talents.Vigor {
		maxEnergy = 110
	}
	rogue.EnableEnergyBar(maxEnergy, func(sim *core.Simulation) {
		rogue.TryUseCooldowns(sim)
		if rogue.GCD.IsReady(sim) {
			rogue.doRotation(sim)
		}
	})
	rogue.EnergyTickMultiplier *= (1 + []float64{0, 0.08, 0.16, 0.25}[rogue.Talents.Vitality])

	rogue.EnableAutoAttacks(rogue, core.AutoAttackOptions{
		MainHand:       rogue.WeaponFromMainHand(0), // Set crit multiplier later when we have targets.
		OffHand:        rogue.WeaponFromOffHand(0),  // Set crit multiplier later when we have targets.
		AutoSwingMelee: true,
	})

	rogue.AddStatDependency(stats.Strength, stats.AttackPower, 1.0+1)
	rogue.AddStatDependency(stats.Agility, stats.AttackPower, 1.0+1)
	rogue.AddStatDependency(stats.Agility, stats.MeleeCrit, 1.0+(core.CritRatingPerCritChance/83.15))

	return rogue
}

func (rogue *Rogue) ApplyCutToTheChase(sim *core.Simulation) {
	if rogue.Talents.CutToTheChase > 0 && rogue.SliceAndDiceAura.IsActive() {
		chanceToRefresh := float64(rogue.Talents.CutToTheChase) * 0.2
		if chanceToRefresh == 1 || sim.RandomFloat("Cut to the Chase") < chanceToRefresh {
			rogue.SliceAndDiceAura.Duration = rogue.sliceAndDiceDurations[5]
			rogue.SliceAndDiceAura.Activate(sim)
		}
	}
}

func init() {
	core.BaseStats[core.BaseStatsKey{Race: proto.Race_RaceBloodElf, Class: proto.Class_ClassRogue}] = stats.Stats{
		stats.Health:    3524,
		stats.Strength:  112,
		stats.Agility:   206,
		stats.Stamina:   88,
		stats.Intellect: 43,
		stats.Spirit:    57,

		stats.AttackPower: 140,
		stats.MeleeCrit:   -0.3 * core.CritRatingPerCritChance,
		stats.SpellCrit:   -0.3 * core.CritRatingPerCritChance,
	}
	core.BaseStats[core.BaseStatsKey{Race: proto.Race_RaceDwarf, Class: proto.Class_ClassRogue}] = stats.Stats{
		stats.Health:    3524,
		stats.Strength:  120,
		stats.Agility:   200,
		stats.Stamina:   92,
		stats.Intellect: 38,
		stats.Spirit:    57,

		stats.AttackPower: 140,
		stats.MeleeCrit:   -0.3 * core.CritRatingPerCritChance,
		stats.SpellCrit:   -0.3 * core.CritRatingPerCritChance,
	}
	core.BaseStats[core.BaseStatsKey{Race: proto.Race_RaceGnome, Class: proto.Class_ClassRogue}] = stats.Stats{
		stats.Health:    3524,
		stats.Strength:  110,
		stats.Agility:   206,
		stats.Stamina:   88,
		stats.Intellect: 45,
		stats.Spirit:    58,

		stats.AttackPower: 140,
		stats.MeleeCrit:   -0.3 * core.CritRatingPerCritChance,
		stats.SpellCrit:   -0.3 * core.CritRatingPerCritChance,
	}
	core.BaseStats[core.BaseStatsKey{Race: proto.Race_RaceHuman, Class: proto.Class_ClassRogue}] = stats.Stats{
		stats.Health:    3524,
		stats.Strength:  115,
		stats.Agility:   204,
		stats.Stamina:   89,
		stats.Intellect: 39,
		stats.Spirit:    58,

		stats.AttackPower: 140,
		stats.MeleeCrit:   -0.3 * core.CritRatingPerCritChance,
		stats.SpellCrit:   -0.3 * core.CritRatingPerCritChance,
	}
	core.BaseStats[core.BaseStatsKey{Race: proto.Race_RaceNightElf, Class: proto.Class_ClassRogue}] = stats.Stats{
		stats.Health:    3524,
		stats.Strength:  111,
		stats.Agility:   208,
		stats.Stamina:   88,
		stats.Intellect: 39,
		stats.Spirit:    58,

		stats.AttackPower: 140,
		stats.MeleeCrit:   -0.3 * core.CritRatingPerCritChance,
		stats.SpellCrit:   -0.3 * core.CritRatingPerCritChance,
	}
	core.BaseStats[core.BaseStatsKey{Race: proto.Race_RaceOrc, Class: proto.Class_ClassRogue}] = stats.Stats{
		stats.Health:    3524,
		stats.Strength:  118,
		stats.Agility:   201,
		stats.Stamina:   91,
		stats.Intellect: 36,
		stats.Spirit:    61,

		stats.AttackPower: 140,
		stats.MeleeCrit:   -0.3 * core.CritRatingPerCritChance,
		stats.SpellCrit:   -0.3 * core.CritRatingPerCritChance,
	}
	core.BaseStats[core.BaseStatsKey{Race: proto.Race_RaceTroll, Class: proto.Class_ClassRogue}] = stats.Stats{
		stats.Health:    3524,
		stats.Strength:  116,
		stats.Agility:   206,
		stats.Stamina:   90,
		stats.Intellect: 35,
		stats.Spirit:    59,

		stats.AttackPower: 140,
		stats.MeleeCrit:   -0.3 * core.CritRatingPerCritChance,
		stats.SpellCrit:   -0.3 * core.CritRatingPerCritChance,
	}
	core.BaseStats[core.BaseStatsKey{Race: proto.Race_RaceUndead, Class: proto.Class_ClassRogue}] = stats.Stats{
		stats.Health:    3524,
		stats.Strength:  114,
		stats.Agility:   202,
		stats.Stamina:   90,
		stats.Intellect: 37,
		stats.Spirit:    63,

		stats.AttackPower: 140,
		stats.MeleeCrit:   -0.3 * core.CritRatingPerCritChance,
		stats.SpellCrit:   -0.3 * core.CritRatingPerCritChance,
	}
}

// Agent is a generic way to access underlying rogue on any of the agents.
type RogueAgent interface {
	GetRogue() *Rogue
}
