import { en, type Dict } from './en';
import { de } from './de';
import { fr } from './fr';
import { es } from './es';
import { it } from './it';
import { pt } from './pt';
import { nl } from './nl';
import { pl } from './pl';
import { ru } from './ru';

// Every shipped dictionary. The language menu is built from these keys, so a
// language is only offered once it is actually translated.
export const DICTS: Record<string, Dict> = { en, de, fr, es, it, pt, nl, pl, ru };
