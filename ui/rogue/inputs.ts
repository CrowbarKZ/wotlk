import { Spec } from '../core/proto/common.js';
import { ActionId } from '../core/proto_utils/action_id.js';

import * as InputHelpers from '../core/components/input_helpers.js';

import {
	Rogue_Rotation_Builder as Builder,
	Rogue_Options_PoisonImbue as Poison,
} from '../core/proto/rogue.js';

// Configuration for spec-specific UI elements on the settings tab.
// These don't need to be in a separate file but it keeps things cleaner.

export const MainHandImbue = InputHelpers.makeSpecOptionsEnumIconInput<Spec.SpecRogue, Poison>({
	fieldName: 'mhImbue',
	numColumns: 1,
	values: [
		{ color: 'grey', value: Poison.NoPoison },
		{ actionId: ActionId.fromItemId(43233), value: Poison.DeadlyPoison },
		{ actionId: ActionId.fromItemId(43231), value: Poison.InstantPoison },
	],
});

export const OffHandImbue = InputHelpers.makeSpecOptionsEnumIconInput<Spec.SpecRogue, Poison>({
	fieldName: 'ohImbue',
	numColumns: 1,
	values: [
		{ color: 'grey', value: Poison.NoPoison },
		{ actionId: ActionId.fromItemId(43233), value: Poison.DeadlyPoison },
		{ actionId: ActionId.fromItemId(43231), value: Poison.InstantPoison },
	],
});

export const RogueRotationConfig = {
	inputs: [
		InputHelpers.makeRotationEnumInput<Spec.SpecRogue, Builder>({
			fieldName: 'builder',
			label: 'Builder',
			values: [
				{
					name: 'Auto', value: Builder.Auto,
					tooltip: 'Automatically selects a builder based on weapons/talents.',
				},
				{ name: 'Sinister Strike', value: Builder.SinisterStrike },
				{ name: 'Mutilate', value: Builder.Mutilate },
			],
		}),
		InputHelpers.makeRotationBooleanInput<Spec.SpecRogue>({
			fieldName: 'maintainExposeArmor',
			label: 'Maintain EA',
			labelTooltip: 'Keeps Expose Armor active on the primary target.',
		}),
		InputHelpers.makeRotationBooleanInput<Spec.SpecRogue>({
			fieldName: 'useRupture',
			label: 'Use Rupture',
			labelTooltip: 'Uses Rupture over Eviscerate when appropriate.',
		}),
		InputHelpers.makeRotationBooleanInput<Spec.SpecRogue>({
			fieldName: 'useEnvenom',
			label: 'Use Envenom',
			labelTooltip: 'Uses Envenom over Eviscerate when appropriate.',
		}),
		InputHelpers.makeRotationBooleanInput<Spec.SpecRogue>({
			fieldName: 'useShiv',
			label: 'Use Shiv',
			labelTooltip: 'Uses Shiv in place of the selected builder if Deadly Poison is about to expire. Requires Deadly Poison in the off-hand.',
		}),
		InputHelpers.makeRotationNumberInput<Spec.SpecRogue>({
			fieldName: 'minComboPointsForDamageFinisher',
			label: 'Min CPs for Damage Finisher',
			labelTooltip: 'Will not use Eviscerate or Rupture unless the Rogue has at least this many Combo Points.',
		}),
	],
};
