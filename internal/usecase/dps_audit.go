package usecase

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/mrcook1e/aion2-global-mcp/internal/domain"
)

type DPSAuditQuery struct {
	Character string  `json:"character,omitempty"`
	Mode      string  `json:"mode,omitempty"`      // pve (default), pvp, abyss
	TargetDef int     `json:"targetDef,omitempty"` // default: 4500
	Duration  float64 `json:"duration,omitempty"`  // default: 23s
}

type DPSAuditService struct {
	charSvc   *CharacterService
	combatSvc *CombatService
	resolver  *Resolver
}

func NewDPSAuditService(charSvc *CharacterService, combatSvc *CombatService, resolver *Resolver) *DPSAuditService {
	return &DPSAuditService{
		charSvc:   charSvc,
		combatSvc: combatSvc,
		resolver:  resolver,
	}
}

func (s *DPSAuditService) AuditCharacterDPS(ctx context.Context, q DPSAuditQuery) (*domain.DPSAuditResult, error) {
	overview, err := s.charSvc.GetCharacterOverview(ctx, q.Character, true)
	if err != nil {
		return nil, fmt.Errorf("ошибка получения профиля персонажа: %w", err)
	}

	mode := strings.ToLower(strings.TrimSpace(q.Mode))
	if mode == "" {
		mode = "pve"
	}

	targetDef := q.TargetDef
	if targetDef <= 0 {
		targetDef = domain.DefaultTargetDef
	}

	ch := &overview.Character

	// Map stats from character
	statMap := make(map[string]float64)
	for _, st := range ch.Stats {
		statMap[st.Name] = st.Value
	}

	// Also extract stats from Genus
	for _, b := range ch.Genus {
		for _, ef := range b.Effects {
			if ef.StatName != "" {
				statMap[ef.StatName] += ef.Value
			}
		}
	}

	// Compute Pipeline
	pipeline := s.computePipeline(statMap, mode, targetDef, true)

	// 1. Audit Gear
	gearAudit := s.auditGear(ch)

	// 2. Audit Skills (Arcane + Stigmas + Daevanion)
	skillAudit := s.auditSkills(ch)

	// 3. Audit Misc (Wings, Pets, Pantheon, Titles, Genus, Monoliths)
	miscAudit := s.auditMisc(ch)

	// 4. Marginal Stat Weights
	weights := s.computeStatWeights(statMap, mode, targetDef, pipeline)

	// 5. Personal Best reference
	var pbRef *domain.PersonalBestDPS
	if s.combatSvc != nil {
		if pbs, err := s.combatSvc.GetPersonalBests(ctx, ch.Key, "", 1); err == nil && len(pbs.Records) > 0 {
			top := pbs.Records[0]
			pbRef = &domain.PersonalBestDPS{
				BossName: top.BossName,
				DPS:      top.DPS,
				Duration: top.DurationSeconds,
				TotalDmg: top.Damage,
			}
		}
	}

	// 6. Synthesize prioritized Bottlenecks
	bottlenecks := s.synthesizeBottlenecks(gearAudit, skillAudit, miscAudit, weights)

	return &domain.DPSAuditResult{
		Character:    ch.Name,
		Class:        ch.Class,
		Level:        ch.Level,
		CombatPower:  ch.CombatPower,
		GearScore:    ch.GearScore,
		Mode:         mode,
		TargetDef:    targetDef,
		Pipeline:     pipeline,
		GearAudit:    gearAudit,
		SkillAudit:   skillAudit,
		MiscAudit:    miscAudit,
		StatWeights:  weights,
		Bottlenecks:  bottlenecks,
		PersonalBest: pbRef,
	}, nil
}

func (s *DPSAuditService) computePipeline(stats map[string]float64, mode string, targetDef int, buffState bool) domain.DPSPipeline {
	getStat := func(keys ...string) float64 {
		for _, k := range keys {
			if v, ok := stats[k]; ok {
				return v
			}
		}
		return 0
	}

	// Pure Attack & Weapon spread
	baseAtk := getStat("Attack", "Атака")
	if baseAtk == 0 {
		baseAtk = 1500
	}
	minAtk := getStat("Min Attack", "Мин. атака")
	maxAtk := getStat("Max Attack", "Макс. атака")
	spreadHalf := math.Max(0, (maxAtk-minAtk)/2.0)

	perfectRating := getStat("Perfect Chance", "Шанс совершенного удара", "Совершенный удар")
	basePerfectProb := math.Min(domain.PerfectCap, perfectRating*domain.PerfectPerRating+(0.10))
	effectivePureAtk := baseAtk + spreadHalf*math.Min(1.0, basePerfectProb)

	// Weapon Damage Boost (1000 = 1.0x)
	wdbRating := getStat("Weapon Damage Boost", "Урон оружия %", "Усиление урона оружия")
	if wdbRating == 0 {
		wdbRating = 1000
	}
	wdbMult := wdbRating / domain.RatingToMult

	// Multi-hit Chance
	mhRating := getStat("Multi-hit Chance", "Шанс серии ударов", "Серия ударов")
	baseMhProb := math.Min(0.85, (mhRating/domain.MultiHitDivisor)+0.18)
	mhMult := 1.0 + baseMhProb*domain.MultiHitAvgDmgBonus

	// Power Shard
	psBonus := getStat("Power Shard Damage Bonus", "Бонус урона осколков силы")
	psBoost := getStat("Power Shard Damage Boost", "Урон осколков силы")
	if psBoost == 0 {
		psBoost = 1000
	}
	shardMult := (psBoost / domain.RatingToMult) * (1.0 + psBonus/domain.RatingToMult)

	// Attack Bonus
	atkBonus := getStat("Attack Bonus", "Бонус атаки")

	// Attack Increase % (Might + Destruction)
	might := getStat("Might", "Сила")
	destruction := getStat("Destruction (Zikel)", "Разрушение (Джикел)", "Destruction")
	buffAtkInc := 0.07 // Vaizel / Bow buff
	atkIncMult := 1.0 + (might+destruction)*domain.DeityAttackScale + buffAtkInc

	// Flat situational attack
	var modeFlatAtk float64
	if mode == "pvp" {
		modeFlatAtk = getStat("PvP Attack", "PvP атака")
	} else {
		modeFlatAtk = getStat("PvE Attack", "PvE атака") + getStat("Boss Attack", "Атака по боссам")
	}
	backAtk := getStat("Back Attack", "Атака со спины") * 0.75 // 75% uptime
	flatSituational := modeFlatAtk + backAtk

	// Phase 1: Total Attack Power
	inner := (effectivePureAtk * wdbMult * mhMult * shardMult) + atkBonus
	totalAttackPower := inner*atkIncMult + flatSituational

	// Phase 2: Defense Mitigation
	penetration := getStat("Penetration", "Пробивание", "Пробивание брони")
	defCoeff := domain.DefCoeffPvE
	if mode == "pvp" {
		defCoeff = domain.DefCoeffPvP
	}
	mitigation := math.Max(0.0, float64(targetDef)-penetration) * defCoeff

	// Phase 3: Buckets
	dmgBoost := getStat("Damage Boost", "Усиление урона", "Урон %")
	var modeBoost float64
	if mode == "pvp" {
		modeBoost = getStat("PvP Damage Boost", "PvP усиление урона")
	} else if mode == "abyss" {
		modeBoost = getStat("PvE Damage Boost", "PvE усиление урона") + getStat("Abyss PvE Damage Boost", "PvE урон в Бездне")
	} else {
		modeBoost = getStat("PvE Damage Boost", "PvE усиление урона")
	}
	dmgBoostBucket := 1.0 + (dmgBoost+modeBoost)/domain.RatingToMult

	// Crit
	critRating := getStat("Critical Hit", "Крит. удар", "Шанс крит. удара") + getStat("Back Attack Critical Hit", "Крит со спины")*0.75
	critDiff := math.Max(0.0, critRating-float64(domain.DefaultTargetCritResist))
	critChance := math.Min(domain.CritCap, math.Max(domain.CritFloor, domain.CritBaseProb+(critDiff/100.0)*domain.CritPer100Diff))

	critDmgRating := getStat("Critical Damage Boost", "Сила крит. удара", "Крит. урон %")
	if critDmgRating == 0 {
		critDmgRating = 2000
	}
	critDmgMult := (critDmgRating / domain.RatingToMult) + 0.20 // buff bonus

	// Double Chance
	doubleRating := getStat("Double Chance", "Шанс двойного удара", "Двойной удар")
	rawDouble := doubleRating / domain.DoubleDivisor
	doubleChance := math.Min(domain.DoubleCap, math.Max(0.0, rawDouble-domain.DefaultTargetDoubleResist))
	doubleExpectedMult := 1.0 + doubleChance

	// Speed & CDR
	combatSpeed := getStat("Combat Speed", "Скорость боя", "Скорость атаки")
	combatSpeedMult := 1.0 + (combatSpeed / 10000.0)
	illusionCDR := getStat("Illusion (Kaisinel)", "Иллюзия (Кайсинель)", "Illusion") * 0.001

	return domain.DPSPipeline{
		EffectivePureAttack: round1(effectivePureAtk),
		WeaponDmgBoost:      round3(wdbMult),
		MultiHitMult:        round3(mhMult),
		PowerShardMult:      round3(shardMult),
		AttackBonus:         round1(atkBonus),
		AttackIncreaseMult:  round3(atkIncMult),
		FlatSituational:     round1(flatSituational),
		TotalAttackPower:    round1(totalAttackPower),
		Penetration:         round1(penetration),
		Mitigation:          round1(mitigation),
		DmgBoostBucket:      round3(dmgBoostBucket),
		CritChance:          round3(critChance * 100),
		CritDmgMult:         round3(critDmgMult),
		DoubleChance:        round3(doubleChance * 100),
		DoubleExpectedMult:  round3(doubleExpectedMult),
		CombatSpeedMult:     round3(combatSpeedMult),
		IllusionCDR:         round3(illusionCDR),
	}
}

// 1. Audit Gear: levels, enchants, exceed, unique stats, manastones
func (s *DPSAuditService) auditGear(ch *domain.Character) domain.GearAudit {
	var issues []domain.GearIssue
	var slotDetails []domain.GearSlotInfo

	for _, g := range ch.Gear {
		var slotIssues []string

		// Check enchant level
		if g.EnchantLevel < 10 {
			iss := domain.GearIssue{
				Slot:     g.Slot,
				ItemName: g.ItemName,
				Severity: "critical",
				Issue:    fmt.Sprintf("Низкая заточка +%d (рекомендуется минимум +12, оптимум +15)", g.EnchantLevel),
				Fix:      "Заточите слот камнями усиления до +12 или выше",
			}
			issues = append(issues, iss)
			slotIssues = append(slotIssues, iss.Issue)
		} else if g.EnchantLevel < 12 {
			iss := domain.GearIssue{
				Slot:     g.Slot,
				ItemName: g.ItemName,
				Severity: "warning",
				Issue:    fmt.Sprintf("Заточка +%d не достигла капа тира (+12..+15)", g.EnchantLevel),
				Fix:      "Добейте заточку до +15 для открытия скрытых бонусов атаки",
			}
			issues = append(issues, iss)
			slotIssues = append(slotIssues, iss.Issue)
		}

		// Check Exceed (breakthrough) for endgame gear (grade >= 41)
		if g.Grade >= domain.GradeHeroic && g.ExceedLevel == 0 {
			iss := domain.GearIssue{
				Slot:     g.Slot,
				ItemName: g.ItemName,
				Severity: "warning",
				Issue:    "Отсутствует прорыв Exceed (0/5) на героическом/реликварном предмете",
				Fix:      "Сделайте прорыв Exceed через дубликат или осколки прорыва для открытия дополнительных аффиксов",
			}
			issues = append(issues, iss)
			slotIssues = append(slotIssues, iss.Issue)
		}

		// Check Grade
		if g.Grade < domain.GradeHeroic && g.ItemLevel < 45 {
			iss := domain.GearIssue{
				Slot:     g.Slot,
				ItemName: g.ItemName,
				Severity: "info",
				Issue:    fmt.Sprintf("Предмет грейда %d (ниже героического Т1)", g.Grade),
				Fix:      "Замените на героическую экипировку из актуальных подземелий Т1/Т2",
			}
			issues = append(issues, iss)
			slotIssues = append(slotIssues, iss.Issue)
		}

		// Check Manastones
		if len(g.ManastoneIDs) == 0 {
			iss := domain.GearIssue{
				Slot:     g.Slot,
				ItemName: g.ItemName,
				Severity: "critical",
				Issue:    "В слоте нет ни одного установленного камня маны",
				Fix:      "Вставьте топовые камни маны на профильный стат (Атака, Крит, Пробивание)",
			}
			issues = append(issues, iss)
			slotIssues = append(slotIssues, iss.Issue)
		}

		slotDetails = append(slotDetails, domain.GearSlotInfo{
			Slot:         g.Slot,
			ItemName:     g.ItemName,
			Grade:        g.Grade,
			ItemLevel:    g.ItemLevel,
			EnchantLevel: g.EnchantLevel,
			ExceedLevel:  g.ExceedLevel,
			Manastones:   g.Manastones,
			Issues:       slotIssues,
		})
	}

	return domain.GearAudit{
		TotalSlots:   len(ch.Gear),
		AuditedSlots: len(slotDetails),
		Issues:       issues,
		SlotDetails:  slotDetails,
	}
}

// 2. Audit Skills: levels, tripods, Daevanion
func (s *DPSAuditService) auditSkills(ch *domain.Character) domain.SkillAudit {
	var issues []domain.SkillIssue
	var skillItems []domain.SkillAuditItem
	totalLvl := 0
	maxLvl := 0
	equippedCount := 0

	for _, sk := range ch.Skills {
		if !sk.Equipped {
			continue
		}
		equippedCount++
		totalLvl += sk.Level
		if sk.Level > maxLvl {
			maxLvl = sk.Level
		}

		var skIssues []string

		// Check skill level
		if sk.Level < 10 {
			iss := domain.SkillIssue{
				SkillName: sk.Name,
				Severity:  "warning",
				Issue:     fmt.Sprintf("Уровень умения %d/20 — плоский урон умения проседает", sk.Level),
				Fix:       "Прокачайте умение книгами мастерства минимум до 10 для открытия второго тира специализации",
			}
			issues = append(issues, iss)
			skIssues = append(skIssues, iss.Issue)
		}

		// Check tripods (specializations)
		if len(sk.Specializations) == 0 {
			iss := domain.SkillIssue{
				SkillName: sk.Name,
				Severity:  "critical",
				Issue:     "Не выбраны триподы специализации для экипированного умения",
				Fix:       "Настройте специализацию умения (выберите ветку на урон/снижение КД)",
			}
			issues = append(issues, iss)
			skIssues = append(skIssues, iss.Issue)
		}

		skillItems = append(skillItems, domain.SkillAuditItem{
			SkillID:         sk.SkillID,
			Name:            sk.Name,
			Level:           sk.Level,
			Specializations: sk.SpecializationDetails,
			Issues:          skIssues,
		})
	}

	avgLvl := 0.0
	if equippedCount > 0 {
		avgLvl = float64(totalLvl) / float64(equippedCount)
	}

	// Daevanion check
	var daevanionBoards []domain.DaevanionBoardSummary
	totalNodes := 0
	for _, b := range ch.Daevanion {
		totalNodes += b.UnlockedCount
		daevanionBoards = append(daevanionBoards, domain.DaevanionBoardSummary{
			BoardID:       b.BoardID,
			UnlockedCount: b.UnlockedCount,
		})
	}

	if totalNodes < 50 {
		issues = append(issues, domain.SkillIssue{
			Severity: "warning",
			Issue:    fmt.Sprintf("Низкий общий прогресс Даэваниона (%d открытых узлов)", totalNodes),
			Fix:      "Фармите фрагменты памяти Даэваниона для закрытия ключевых узлов атаки и крит урона",
		})
	}

	return domain.SkillAudit{
		EquippedCount: equippedCount,
		TotalCount:    len(ch.Skills),
		MaxSkillLevel: maxLvl,
		AvgSkillLevel: round1(avgLvl),
		Skills:        skillItems,
		DaevanionAudit: domain.DaevanionAudit{
			TotalNodes:    totalNodes,
			BoardsSummary: daevanionBoards,
		},
		Issues: issues,
	}
}

// 3. Audit Misc: Wings, Pets, Pantheon, Titles, Genus, Monoliths
func (s *DPSAuditService) auditMisc(ch *domain.Character) domain.MiscAudit {
	var issues []domain.MiscIssue

	// Wings
	var wingEntry *domain.MiscEntry
	if ch.EquippedWing != nil && ch.EquippedWing.ID > 0 {
		wingEntry = &domain.MiscEntry{
			ID:   ch.EquippedWing.ID,
			Name: ch.EquippedWing.Name,
		}
	} else {
		issues = append(issues, domain.MiscIssue{
			Category: "wing",
			Severity: "critical",
			Issue:    "Не экипированы крылья",
			Fix:      "Наденьте топовые крылья с бонусом к скорости полета, атаке и PvP/PvE урону",
		})
	}

	// Titles
	var titleEntries []domain.MiscEntry
	for _, t := range ch.EquippedTitles {
		titleEntries = append(titleEntries, domain.MiscEntry{
			ID:   t.ID,
			Name: t.Name,
		})
	}
	if len(titleEntries) == 0 {
		issues = append(issues, domain.MiscIssue{
			Category: "title",
			Severity: "warning",
			Issue:    "Не активирован боевой титул",
			Fix:      "Активируйте титул с максимальной прибавкой к Pure Attack или Crit Hit",
		})
	}

	// Genus branches
	var genusAudits []domain.GenusAudit
	for _, b := range ch.Genus {
		var branchIssues []string
		junkCount := 0
		for _, ef := range b.Effects {
			// Flag junk defensive/utility stats in offensive build
			nameLower := strings.ToLower(ef.StatName)
			if strings.Contains(nameLower, "регенерация") || strings.Contains(nameLower, "сопротивление") || strings.Contains(nameLower, "макс. hp") {
				junkCount++
			}
		}
		if junkCount >= 2 {
			issText := fmt.Sprintf("В ветке %s присутствуют %d защитных/мусорных роллов вместо атакующих", b.Name, junkCount)
			branchIssues = append(branchIssues, issText)
			issues = append(issues, domain.MiscIssue{
				Category: "genus",
				Severity: "warning",
				Issue:    issText,
				Fix:      fmt.Sprintf("Переролльте слоты ветки %s в сторону Attack, Damage Boost, Double Chance или Penetration", b.Name),
			})
		}
		genusAudits = append(genusAudits, domain.GenusAudit{
			Branch:  b.Name,
			Level:   b.Level,
			Effects: b.Effects,
			Issues:  branchIssues,
		})
	}

	// Monoliths
	var monolithAudits []domain.MonolithAudit
	for _, m := range ch.Monoliths {
		monolithAudits = append(monolithAudits, domain.MonolithAudit{
			MonolithID: m.MonolithID,
			Name:       m.Name,
			Tokens:     m.Tokens,
		})
	}

	// Pantheon
	var pantheonAudits []domain.PantheonAudit
	for _, p := range ch.Pantheon {
		pantheonAudits = append(pantheonAudits, domain.PantheonAudit{
			Slot:   p.Slot,
			DecoID: p.DecoID,
			Name:   p.Name,
		})
	}
	if len(pantheonAudits) < 4 {
		issues = append(issues, domain.MiscIssue{
			Category: "pantheon",
			Severity: "warning",
			Issue:    fmt.Sprintf("В Пантеоне занято только %d слотов реликвий из доступных", len(pantheonAudits)),
			Fix:      "Установите недостающие реликвии Владык (Джикел, Байзел, Неджакан, Кайсинель) для пассивных мультипликаторов",
		})
	}

	// Pets
	var petAudit *domain.PetAudit
	if len(ch.Pets) > 0 {
		topPet := ch.Pets[0]
		petAudit = &domain.PetAudit{
			OwnedCount:  len(ch.Pets),
			TopPetName:  topPet.Name,
			TopPetLevel: topPet.Level,
		}
		if topPet.Level < 30 {
			issues = append(issues, domain.MiscIssue{
				Category: "pet",
				Severity: "info",
				Issue:    fmt.Sprintf("Уровень основного питомца %d (кап 50)", topPet.Level),
				Fix:      "Кормите питомца для прокачки его боевых пассивок и резонанса",
			})
		}
	} else if ch.OwnedPetsCount == 0 {
		issues = append(issues, domain.MiscIssue{
			Category: "pet",
			Severity: "warning",
			Issue:    "Коллекция питомцев пуста",
			Fix:      "Призовите боевого питомца для получения командного бонуса к статам",
		})
	}

	return domain.MiscAudit{
		Wing:      wingEntry,
		Titles:    titleEntries,
		Genus:     genusAudits,
		Monoliths: monolithAudits,
		Pantheon:  pantheonAudits,
		Pets:      petAudit,
		Issues:    issues,
	}
}

// 4. Marginal Stat Weights
func (s *DPSAuditService) computeStatWeights(baseStats map[string]float64, mode string, targetDef int, basePipe domain.DPSPipeline) []domain.StatWeight {
	// Baseline theoretical hit value
	baseAtk := basePipe.TotalAttackPower
	baseMit := basePipe.Mitigation
	baseDmg := math.Max(1.0, (baseAtk*2.5-baseMit)) * basePipe.DmgBoostBucket * basePipe.CritDmgMult * basePipe.DoubleExpectedMult

	type testCase struct {
		label  string
		stat   string
		delta  float64
		bucket string
	}

	tests := []testCase{
		{"Double Chance (+1% / +24 рейт.)", "Double Chance", 24, "Независимая корзина Double"},
		{"Weapon Damage Boost (+1% / +10 рейт.)", "Weapon Damage Boost", 10, "Множитель Pure Attack (Фаза 1)"},
		{"Might / Zikel (+10 ед. = +1% Attack Inc)", "Might", 10, "Множитель всей цепочки атаки"},
		{"Critical Damage Boost (+1% / +10 рейт.)", "Critical Damage Boost", 10, "Корзина крит. урона"},
		{"Pure Attack (+10 ед. Attack)", "Attack", 10, "База Pure Attack (умножается на WDB)"},
		{"Damage / PvE Damage Boost (+10 ед.)", "Damage Boost", 10, "Общая корзина (с учетом размывания)"},
		{"Penetration (+100 ед. Пробивания)", "Penetration", 100, "Снижение брони (-10 урона до корзин)"},
		{"Combat Speed (+1% / +100 рейт.)", "Combat Speed", 100, "Частота анимаций и автоатак"},
	}

	var results []domain.StatWeight
	for _, tc := range tests {
		copied := make(map[string]float64, len(baseStats))
		for k, v := range baseStats {
			copied[k] = v
		}
		copied[tc.stat] += tc.delta

		newPipe := s.computePipeline(copied, mode, targetDef, true)
		newAtk := newPipe.TotalAttackPower
		newMit := newPipe.Mitigation
		newDmg := math.Max(1.0, (newAtk*2.5-newMit)) * newPipe.DmgBoostBucket * newPipe.CritDmgMult * newPipe.DoubleExpectedMult

		deltaDPS := newDmg - baseDmg
		pctGain := 0.0
		if baseDmg > 0 {
			pctGain = ((newDmg / baseDmg) - 1.0) * 100.0
		}

		results = append(results, domain.StatWeight{
			Stat:     tc.label,
			DeltaDPS: round1(deltaDPS),
			PctGain:  round3(pctGain),
			Bucket:   tc.bucket,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].PctGain > results[j].PctGain
	})

	return results
}

// 5. Synthesize prioritized Bottlenecks
func (s *DPSAuditService) synthesizeBottlenecks(gear domain.GearAudit, skills domain.SkillAudit, misc domain.MiscAudit, weights []domain.StatWeight) []domain.Bottleneck {
	var bottlenecks []domain.Bottleneck
	rank := 1

	// Critical gear issues first
	for _, gi := range gear.Issues {
		if gi.Severity == "critical" {
			bottlenecks = append(bottlenecks, domain.Bottleneck{
				Rank:     rank,
				Category: "gear",
				Severity: gi.Severity,
				Issue:    fmt.Sprintf("Слот [%s]: %s", gi.ItemName, gi.Issue),
				Fix:      gi.Fix,
				Impact:   "Прямая потеря 5–15% базового урона слота",
			})
			rank++
		}
	}

	// Critical skill issues
	for _, si := range skills.Issues {
		if si.Severity == "critical" {
			bottlenecks = append(bottlenecks, domain.Bottleneck{
				Rank:     rank,
				Category: "skill",
				Severity: si.Severity,
				Issue:    fmt.Sprintf("Умение [%s]: %s", si.SkillName, si.Issue),
				Fix:      si.Fix,
				Impact:   "Потеря до 30–50% урона ротации от неактивных триподов",
			})
			rank++
		}
	}

	// Critical misc issues
	for _, mi := range misc.Issues {
		if mi.Severity == "critical" {
			bottlenecks = append(bottlenecks, domain.Bottleneck{
				Rank:     rank,
				Category: "misc",
				Severity: mi.Severity,
				Issue:    fmt.Sprintf("[%s]: %s", mi.Category, mi.Issue),
				Fix:      mi.Fix,
				Impact:   "Критическая потеря статов целого подсистемного источника",
			})
			rank++
		}
	}

	// Warning gear issues
	for _, gi := range gear.Issues {
		if gi.Severity == "warning" {
			bottlenecks = append(bottlenecks, domain.Bottleneck{
				Rank:     rank,
				Category: "gear",
				Severity: gi.Severity,
				Issue:    fmt.Sprintf("Слот [%s]: %s", gi.ItemName, gi.Issue),
				Fix:      gi.Fix,
				Impact:   "Потеря дополнительных аффиксов и масштабирования Pure Attack",
			})
			rank++
		}
	}

	// Warning skill issues (Daevanion, low levels)
	for _, si := range skills.Issues {
		if si.Severity == "warning" {
			bottlenecks = append(bottlenecks, domain.Bottleneck{
				Rank:     rank,
				Category: "skill",
				Severity: si.Severity,
				Issue:    si.Issue,
				Fix:      si.Fix,
				Impact:   "Просадка плоского скалирования умений за уровень",
			})
			rank++
		}
	}

	// Warning misc issues (Genus, Pantheon)
	for _, mi := range misc.Issues {
		if mi.Severity == "warning" {
			bottlenecks = append(bottlenecks, domain.Bottleneck{
				Rank:     rank,
				Category: "misc",
				Severity: mi.Severity,
				Issue:    fmt.Sprintf("[%s]: %s", mi.Category, mi.Issue),
				Fix:      mi.Fix,
				Impact:   "Размывание мультипликативных корзин мусорными защитными роллами",
			})
			rank++
		}
	}

	// Stat weight recommendation as strategic priority
	if len(weights) > 0 {
		topStat := weights[0]
		bottlenecks = append(bottlenecks, domain.Bottleneck{
			Rank:     rank,
			Category: "stat_priority",
			Severity: "info",
			Issue:    fmt.Sprintf("Наивысший приоритет статов: %s (+%.2f%% DPS)", topStat.Stat, topStat.PctGain),
			Fix:      fmt.Sprintf("При выборе роллов в Генусе, камней маны и крафта фокусируйтесь на %s", topStat.Stat),
			Impact:   "Максимальная отдача на единицу инвестиций характеристик",
		})
	}

	return bottlenecks
}

func round1(val float64) float64 {
	return math.Round(val*10) / 10
}

func round3(val float64) float64 {
	return math.Round(val*1000) / 1000
}
