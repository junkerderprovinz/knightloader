import { useEffect, useRef, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import type { SVGProps } from 'react';
import { Button, Card, ErrorCard, IconBadge, InfoBubble, LoadingCard, PageHeader, SectionTitle, TextInput, Toggle } from '../../components/ui';
import {
  RuleEditor,
  Segments,
  actionLabel,
  emptyRule,
  parseSize,
  ruleSummary,
  useRx,
  type Flavour,
  type Grammar,
  type Problem,
  type Report,
  type Rule,
  type RuleSet,
} from '../../components/RuleEditor';
import { IconArrowDown, IconArrowUp, IconPlus, IconTrash } from '../../lib/icons';
import type { Category as Drawer } from '../../lib/api';
import { useT } from '../../lib/i18n';
import { CategoriesCard } from './Categories';
import { useDraft } from './context';
import { NeutralSwitch } from './controls';

/**
 * Rules edits the Packagizer and the link filter, one engine used twice. The
 * lists are part of the settings draft; the grammar and dry-run routes store
 * nothing. Compile runs on every edit and its problems are drawn on the rule
 * and condition that caused them, and a test box shows what a pasted link
 * would do before anything is saved. The categories follow on the same page,
 * since a Packagizer rule naming a category that does not exist is refused.
 */

const FIELD: Record<Flavour, 'packagizer' | 'linkFilter'> = {
  packagizer: 'packagizer',
  filter: 'linkFilter',
};

/** lib/api.ts's Settings does not declare the two rule sets, hence the casts. */
function readSet(cfg: unknown, flavour: Flavour): RuleSet {
  return (cfg as Record<string, RuleSet | undefined>)[FIELD[flavour]] ?? {};
}

interface Sample {
  url: string;
  filename: string;
  source: string;
  size: string;
  pkg: string;
}

const emptySample = (): Sample => ({ url: '', filename: '', source: '', size: '', pkg: '' });

const IconDuplicate = (p: SVGProps<SVGSVGElement>) => (
  <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.4" aria-hidden {...p}>
    <rect x="5.5" y="5.5" width="8" height="8" rx="1.6" />
    <path d="M10.5 2.5H4a1.5 1.5 0 0 0-1.5 1.5v6.5" />
  </svg>
);

export function Rules() {
  const { t } = useT();
  return (
    <div className="flex flex-col gap-10">
      <PageHeader title={t('settings.nav.rules')} />
      <RuleCards />
      <CategoriesCard hue={3} />
    </div>
  );
}

/** RuleCards draws the setup, the list and the test box, on hues 0 to 2. */
function RuleCards() {
  const rx = useRx();
  const { cfg, patch } = useDraft();

  const [flavour, setFlavour] = useState<Flavour>('packagizer');
  const [grammar, setGrammar] = useState<Grammar | null>(null);
  const [grammarError, setGrammarError] = useState('');
  const [openRule, setOpenRule] = useState(-1);
  const [samples, setSamples] = useState<Sample[]>([emptySample()]);
  const [report, setReport] = useState<Report | null>(null);
  const [previewError, setPreviewError] = useState('');
  const [notice, setNotice] = useState('');
  const fileInput = useRef<HTMLInputElement>(null);

  const set = readSet(cfg, flavour);
  const rules = set.rules ?? [];

  const write = (next: RuleSet) =>
    patch({ [FIELD[flavour]]: next } as unknown as Parameters<typeof patch>[0]);
  const writeRules = (next: Rule[]) => write({ ...set, rules: next });

  // The grammar comes from the engine, so the form offers only what it accepts.
  useEffect(() => {
    let live = true;
    fetch('/api/rules/grammar')
      .then((r) => (r.ok ? r.json() : Promise.reject(new Error(String(r.status)))))
      .then((g: Grammar) => live && setGrammar(g))
      .catch((e: unknown) => live && setGrammarError(e instanceof Error ? e.message : String(e)));
    return () => {
      live = false;
    };
  }, []);

  // A rule chip in the task panel links here by name, since a rule has no id.
  const [params, setParams] = useSearchParams();
  const wanted = params.get('rule');
  useEffect(() => {
    if (!wanted) return;
    // The server names an unnamed rule `rule N` in English and lower case, so
    // the translated label would never match.
    const nameAt = (r: Rule, i: number) => r.name?.trim() || `rule ${i + 1}`;
    let hit = false;
    // Both sets write into the same matchedRules field.
    for (const f of ['packagizer', 'filter'] as Flavour[]) {
      const i = (readSet(cfg, f).rules ?? []).findIndex((r, j) => nameAt(r, j) === wanted);
      if (i >= 0) {
        setFlavour(f);
        setOpenRule(i);
        setReport(null);
        hit = true;
        break;
      }
    }
    // A renamed rule, or an unnamed one that has moved, cannot be found; say so.
    if (!hit) setNotice(rx('settings.rules.notFound', { name: wanted }));
    // A one-shot instruction; left in the address it would reopen the rule on
    // every draft edit.
    setParams(
      (p) => {
        const n = new URLSearchParams(p);
        n.delete('rule');
        return n;
      },
      { replace: true },
    );
  }, [wanted, cfg, rx, setParams]);

  // The dry run, debounced, on every edit to the set or the samples. Serialised,
  // because the draft hands back a fresh object when the field is absent.
  const setJSON = JSON.stringify(set);
  const linksJSON = JSON.stringify(
    samples
      .filter((s) => s.url.trim() !== '' || s.filename.trim() !== '')
      .map((s) => ({
        url: s.url.trim(),
        filename: s.filename.trim(),
        source: s.source.trim(),
        package: s.pkg.trim(),
        filesize: parseSize(s.size) ?? 0,
      })),
  );

  useEffect(() => {
    const ctl = new AbortController();
    const timer = setTimeout(() => {
      fetch('/api/rules/preview', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: `{"set":${setJSON},"links":${linksJSON}}`,
        signal: ctl.signal,
      })
        .then((r) => (r.ok ? r.json() : r.text().then((t) => Promise.reject(new Error(t.trim())))))
        .then((rep: Report) => {
          setReport(rep);
          setPreviewError('');
        })
        .catch((e: unknown) => {
          // The effect aborting its own request is not a failure.
          if (e instanceof DOMException && e.name === 'AbortError') return;
          setPreviewError(e instanceof Error ? e.message : String(e));
        });
    }, 250);
    return () => {
      clearTimeout(timer);
      ctl.abort();
    };
  }, [setJSON, linksJSON]);

  if (grammarError) {
    return <ErrorCard message={rx('settings.rules.testFailed', { reason: grammarError })} />;
  }
  if (!grammar) {
    return <LoadingCard label={rx('settings.rules.testRunning')} />;
  }

  const problemsFor = (index: number): Problem[] =>
    report?.rules.find((r) => r.index === index)?.problems ?? [];

  const move = (index: number, by: number) => {
    const to = index + by;
    if (to < 0 || to >= rules.length) return;
    const next = [...rules];
    [next[index], next[to]] = [next[to], next[index]];
    writeRules(next);
    // The open rule follows its row.
    if (openRule === index) setOpenRule(to);
    else if (openRule === to) setOpenRule(index);
  };

  const duplicate = (index: number) => {
    const copy: Rule = JSON.parse(JSON.stringify(rules[index]));
    copy.name = `${copy.name || rx('settings.rules.unnamed', { n: index + 1 })} (2)`;
    writeRules([...rules.slice(0, index + 1), copy, ...rules.slice(index + 1)]);
    setOpenRule(index + 1);
  };

  const removeAt = (index: number) => {
    writeRules(rules.filter((_, i) => i !== index));
    setOpenRule(-1);
  };

  const add = () => {
    writeRules([...rules, emptyRule(flavour)]);
    setOpenRule(rules.length);
  };

  function exportJSON() {
    const blob = new Blob([JSON.stringify(set, null, 2)], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `knightloader-${FIELD[flavour]}.json`;
    a.click();
    URL.revokeObjectURL(url);
  }

  async function importJSON(file: File) {
    setNotice('');
    try {
      const parsed = parseRuleSet(await file.text());
      write(parsed);
      setOpenRule(-1);
      setNotice(rx('settings.rules.importedCount', { n: parsed.rules?.length ?? 0 }));
    } catch (e: unknown) {
      setNotice(rx('settings.rules.importFailed', { reason: e instanceof Error ? e.message : String(e) }));
    }
  }

  const on = !set.disabled;

  return (
    <>
      <Card hue={0} className="flex flex-col gap-4">
        <SectionTitle>{rx('settings.rules.setupTitle')}</SectionTitle>
        <div className="flex flex-wrap items-center gap-4">
          <Segments
            label={rx('settings.rules.flavourLabel')}
            value={flavour}
            onChange={(f) => {
              setFlavour(f);
              setOpenRule(-1);
              // The old list's report would mark the new list's rules.
              setReport(null);
            }}
            options={[
              { value: 'packagizer', label: rx('settings.rules.flavour.packagizer') },
              { value: 'filter', label: rx('settings.rules.flavour.filter') },
            ]}
          />
          <span className="flex-1" />
          {/* The one accent switch on the page, since it says whether the
              engine does anything at all. */}
          <Toggle
            checked={on}
            onChange={(v) => write({ ...set, disabled: !v })}
            label={on ? rx('settings.rules.setOn') : rx('settings.rules.setOff')}
          />
          <InfoBubble tip={rx('settings.rules.setSwitchHint')} />
        </div>

        {/* The extra space above ties the sentence to the switch below. */}
        <p className="mt-2 text-[11px] leading-snug text-carbon-textSub">
          {flavour === 'packagizer' ? rx('settings.rules.packagizerHint') : rx('settings.rules.filterHint')}
        </p>

        <div className="flex items-center gap-2.5">
          <NeutralSwitch
            on={Boolean(set.stopAfterMatch)}
            onChange={(v) => write({ ...set, stopAfterMatch: v })}
            name={rx('settings.rules.stopAfterMatch')}
          />
          <span className="flex items-center text-xs text-carbon-textSub">
            {rx('settings.rules.stopAfterMatch')}
            <InfoBubble tip={rx('settings.rules.stopHint')} />
          </span>
        </div>
      </Card>

      <Card hue={1} className="flex flex-col gap-4">
        <SectionTitle
          right={
            <div className="flex items-center gap-2">
              {/* One bubble per button, since Import and Export do different things. */}
              <Button kind="secondary" onClick={() => fileInput.current?.click()}>
                {rx('settings.rules.import')}
              </Button>
              <InfoBubble tip={rx('settings.rules.importTitle')} />
              <Button kind="secondary" onClick={exportJSON}>
                {rx('settings.rules.export')}
              </Button>
              <InfoBubble tip={rx('settings.rules.exportTitle')} />
              <Button icon={<IconPlus width={16} height={16} />} onClick={add}>
                {rx('settings.rules.add')}
              </Button>
            </div>
          }
        >
          {rx('settings.rules.listTitle')}
        </SectionTitle>

        <input
          ref={fileInput}
          type="file"
          accept="application/json,.json"
          className="hidden"
          onChange={(e) => {
            const f = e.target.files?.[0];
            // Cleared so picking the same file again fires a change event.
            e.target.value = '';
            if (f) void importJSON(f);
          }}
        />

        {notice && <p className="text-[11px] text-carbon-textSub">{notice}</p>}
        {previewError && (
          <p className="text-[11px] text-statusFail">{rx('settings.rules.testFailed', { reason: previewError })}</p>
        )}

        {rules.length === 0 ? (
          // Inside the card rather than an EmptyState, which would hide Add.
          <p className="py-6 text-center text-sm text-carbon-textSub">
            {rx('settings.rules.empty')}
            <span className="mt-1 block text-[11px] text-carbon-textMuted">
              {flavour === 'packagizer'
                ? rx('settings.rules.emptyPackagizer')
                : rx('settings.rules.emptyFilter')}
            </span>
          </p>
        ) : (
          <ul className="flex flex-col">
            {rules.map((rule, i) => (
              <RuleRow
                key={i}
                rule={rule}
                index={i}
                last={i === rules.length - 1}
                open={openRule === i}
                flavour={flavour}
                grammar={grammar}
                problems={problemsFor(i)}
                matched={report?.rules.find((r) => r.index === i)?.matched ?? 0}
                samples={report?.links.length ?? 0}
                categories={cfg.categories ?? []}
                onToggle={() => setOpenRule(openRule === i ? -1 : i)}
                onChange={(next) => writeRules(rules.map((r, j) => (j === i ? next : r)))}
                onMove={(by) => move(i, by)}
                onDuplicate={() => duplicate(i)}
                onRemove={() => removeAt(i)}
              />
            ))}
          </ul>
        )}
      </Card>

      <TestBox
        flavour={flavour}
        samples={samples}
        setSamples={setSamples}
        report={report}
        downloadDir={(cfg as { downloadDir?: string }).downloadDir ?? ''}
      />
    </>
  );
}

/**
 * parseRuleSet accepts a whole set or a bare array of rules. The shape check
 * stays shallow, since the dry run reports every unusable rule right after.
 */
function parseRuleSet(text: string): RuleSet {
  const raw: unknown = JSON.parse(text);
  const set: unknown = Array.isArray(raw) ? { rules: raw } : raw;
  if (!set || typeof set !== 'object') throw new Error('not an object');
  const rules = (set as RuleSet).rules;
  if (!Array.isArray(rules)) throw new Error('no rules in it');
  for (const r of rules) {
    if (!r || typeof r !== 'object') throw new Error('a rule is not an object');
    if (r.conditions !== undefined && !Array.isArray(r.conditions)) {
      throw new Error('a rule’s conditions are not a list');
    }
  }
  // Only the three fields this page owns, so unknown fields are not saved.
  const s = set as RuleSet;
  return { rules, disabled: Boolean(s.disabled), stopAfterMatch: Boolean(s.stopAfterMatch) };
}

function RuleRow({
  rule,
  index,
  last,
  open,
  flavour,
  grammar,
  problems,
  matched,
  samples,
  categories,
  onToggle,
  onChange,
  onMove,
  onDuplicate,
  onRemove,
}: {
  rule: Rule;
  index: number;
  last: boolean;
  open: boolean;
  flavour: Flavour;
  grammar: Grammar;
  problems: Problem[];
  matched: number;
  samples: number;
  categories: Drawer[];
  onToggle: () => void;
  onChange: (next: Rule) => void;
  onMove: (by: number) => void;
  onDuplicate: () => void;
  onRemove: () => void;
}) {
  const rx = useRx();
  const broken = problems.length > 0;

  return (
    <li className={last ? '' : 'border-b border-carbon-border/60'}>
      <div className="grid grid-cols-[auto_1fr_auto] items-center gap-3 py-2.5">
        <NeutralSwitch
          on={!rule.disabled}
          onChange={(v) => onChange({ ...rule, disabled: !v })}
          name={rule.disabled ? rx('settings.rules.ruleOff') : rx('settings.rules.ruleOn')}
          hue={index}
        />
        <button
          type="button"
          onClick={onToggle}
          aria-expanded={open}
          className="flex min-w-0 items-center gap-3 text-left"
        >
          <span className="glim-num w-5 shrink-0 text-xs text-carbon-textMuted">{index + 1}</span>
          <span className="min-w-0 flex-1">
            <span className="block truncate text-sm text-carbon-text">
              {rule.name?.trim() || rx('settings.rules.unnamed', { n: index + 1 })}
            </span>
            <span className="block truncate text-[11px] text-carbon-textMuted">
              {ruleSummary(rx, rule, flavour)}
            </span>
          </span>
          {/* Shown only once there are samples, or "0 of 0" reads as broken. */}
          {samples > 0 && !broken && (
            <span className="glim-num hidden shrink-0 text-[11px] text-carbon-textMuted sm:block">
              {rx('settings.rules.matchedCount', { n: matched, total: samples })}
            </span>
          )}
          {/* Red reports the rule's state; it sits inside the expand button,
              whose own classes carry no status colour. */}
          {broken && (
            <span className="shrink-0 rounded-[var(--radius-control)] bg-statusFailBg px-2 py-0.5 text-[11px] text-statusFail">
              {problems.length === 1
                ? rx('settings.rules.problemOne')
                : rx('settings.rules.problemCount', { n: problems.length })}
            </span>
          )}
        </button>
        {/* `labelled`, so the actions follow the Beschriftung setting; the name
            and summary truncate instead. */}
        <div className="flex items-center gap-1.5">
          <IconBadge
            labelled
            icon={<IconArrowUp width={16} height={16} />}
            hue={index}
            title={rx('settings.rules.moveUp')}
            aria-label={rx('settings.rules.moveUp')}
            disabled={index === 0}
            onClick={() => onMove(-1)}
          />
          <IconBadge
            labelled
            icon={<IconArrowDown width={16} height={16} />}
            hue={index}
            title={rx('settings.rules.moveDown')}
            aria-label={rx('settings.rules.moveDown')}
            disabled={last}
            onClick={() => onMove(1)}
          />
          <IconBadge
            labelled
            icon={<IconDuplicate width={16} height={16} />}
            hue={index}
            title={rx('settings.rules.duplicate')}
            aria-label={rx('settings.rules.duplicate')}
            onClick={onDuplicate}
          />
          <IconBadge
            labelled
            icon={<IconTrash width={16} height={16} />}
            hue={index}
            title={rx('settings.rules.remove')}
            aria-label={rx('settings.rules.remove')}
            onClick={onRemove}
          />
        </div>
      </div>

      {/* Shown on a closed rule too, so a broken one is found without opening
          each. The engine drops a rule with any problem whole. */}
      {broken && !open && (
        <p className="pb-2.5 ps-12 text-[11px] text-statusFail">{rx('settings.rules.notRunning')}</p>
      )}

      {open && (
        <div className="glim-well mb-3 flex flex-col gap-4 p-4">
          {broken && <p className="text-[11px] text-statusFail">{rx('settings.rules.notRunning')}</p>}
          <RuleEditor
            rule={rule}
            flavour={flavour}
            grammar={grammar}
            problems={problems}
            categories={categories}
            onChange={onChange}
          />
        </div>
      )}
    </li>
  );
}

/**
 * TestBox shows which rules match a pasted link and file name, in order, and
 * what comes out. It runs the list as edited through rules.Preview, on a
 * throwaway Matcher so a preview cannot rename the next real download.
 */
function TestBox({
  flavour,
  samples,
  setSamples,
  report,
  downloadDir,
}: {
  flavour: Flavour;
  samples: Sample[];
  setSamples: (next: Sample[]) => void;
  report: Report | null;
  downloadDir: string;
}) {
  const rx = useRx();
  const update = (i: number, fields: Partial<Sample>) =>
    setSamples(samples.map((s, j) => (j === i ? { ...s, ...fields } : s)));

  return (
    <Card hue={2} className="flex flex-col gap-4">
      <SectionTitle
        right={
          <Button kind="secondary" icon={<IconPlus width={16} height={16} />} onClick={() => setSamples([...samples, emptySample()])}>
            {rx('settings.rules.testAdd')}
          </Button>
        }
      >
        <span className="flex items-center">
          {rx('settings.rules.testTitle')}
          <InfoBubble tip={rx('settings.rules.testHint')} />
        </span>
      </SectionTitle>

      {samples.map((s, i) => (
        <div key={i} className="flex flex-col gap-2 border-b border-carbon-border/60 pb-4 last:border-b-0 last:pb-0">
          <div className="flex items-start gap-2">
            <div className="grid flex-1 gap-2 sm:grid-cols-2">
              <TextInput
                dir="ltr"
                aria-label={rx('settings.rules.testUrl')}
                placeholder={rx('settings.rules.testUrl')}
                value={s.url}
                onChange={(e) => update(i, { url: e.target.value })}
              />
              <TextInput
                dir="ltr"
                aria-label={rx('settings.rules.testFilename')}
                placeholder={rx('settings.rules.testFilename')}
                value={s.filename}
                onChange={(e) => update(i, { filename: e.target.value })}
              />
              <TextInput
                dir="ltr"
                aria-label={rx('settings.rules.testSource')}
                placeholder={rx('settings.rules.testSource')}
                value={s.source}
                onChange={(e) => update(i, { source: e.target.value })}
              />
              <div className="flex gap-2">
                <TextInput
                  dir="ltr"
                  aria-label={rx('settings.rules.testSize')}
                  placeholder={rx('settings.rules.testSize')}
                  value={s.size}
                  onChange={(e) => update(i, { size: e.target.value })}
                />
                <TextInput
                  aria-label={rx('settings.rules.testPackage')}
                  placeholder={rx('settings.rules.testPackage')}
                  value={s.pkg}
                  onChange={(e) => update(i, { pkg: e.target.value })}
                />
              </div>
            </div>
            {/* The row's own badge, with its tooltip and the Beschriftung setting. */}
            <IconBadge
              labelled
              hue={2}
              className="shrink-0"
              icon={<IconTrash width={16} height={16} />}
              title={rx('settings.rules.testRemove')}
              aria-label={rx('settings.rules.testRemove')}
              disabled={samples.length === 1}
              onClick={() => setSamples(samples.filter((_, j) => j !== i))}
            />
          </div>
          <div className="flex items-center gap-3 text-[11px] text-carbon-textMuted">
            <span className="flex items-center">
              {rx('settings.rules.testSource')}
              <InfoBubble tip={rx('settings.rules.testSourceHint')} />
            </span>
            <span className="flex items-center">
              {rx('settings.rules.testSize')}
              <InfoBubble tip={rx('settings.rules.sizeHint')} />
            </span>
            <span className="flex items-center">
              {rx('settings.rules.testPackage')}
              <InfoBubble tip={rx('settings.rules.testPackageHint')} />
            </span>
          </div>
        </div>
      ))}

      <Outcomes flavour={flavour} report={report} downloadDir={downloadDir} />
    </Card>
  );
}

function Outcomes({
  flavour,
  report,
  downloadDir,
}: {
  flavour: Flavour;
  report: Report | null;
  downloadDir: string;
}) {
  const rx = useRx();
  if (!report || report.links.length === 0) {
    return <p className="text-[11px] text-carbon-textMuted">{rx('settings.rules.testEmpty')}</p>;
  }

  return (
    <ul className="flex flex-col gap-3">
      {report.links.map((l, i) => {
        const names = (l.matched ?? []).map(
          (idx) => report.rules.find((r) => r.index === idx)?.name ?? String(idx + 1),
        );
        const rejected = flavour === 'filter' && l.verdict.rejected;
        // The editor's own labels, so the preview names settings the same way.
        const extras: string[] = [];
        if (l.effect.comment) extras.push(`${actionLabel(rx, 'comment')}: ${l.effect.comment}`);
        if (l.effect.priority !== undefined) {
          extras.push(`${actionLabel(rx, 'priority')} ${l.effect.priority}`);
        }
        if (l.effect.chunks !== undefined) extras.push(`${actionLabel(rx, 'chunks')} ${l.effect.chunks}`);
        if (l.effect.autoExtract !== undefined) {
          extras.push(
            `${actionLabel(rx, 'autoExtract')} ${
              l.effect.autoExtract ? rx('settings.rules.yes') : rx('settings.rules.no')
            }`,
          );
        }

        return (
          <li key={i} className="glim-well flex flex-col gap-1.5 p-3 text-xs">
            <div className="flex flex-wrap items-center gap-2">
              <span
                className={`rounded-[var(--radius-control)] px-2 py-0.5 text-[11px] ${
                  rejected ? 'bg-statusFailBg text-statusFail' : 'text-carbon-textSub'
                }`}
              >
                {rejected ? rx('settings.rules.resultRejected') : rx('settings.rules.resultAccepted')}
              </span>
              <span dir="ltr" className="min-w-0 flex-1 truncate text-carbon-textMuted">
                {l.filename || l.url}
              </span>
            </div>

            {rejected && (
              <p className="text-[11px] text-statusFail">
                {l.verdict.reason}
                {/* The engine's own reason already names the rule. */}
                {l.verdict.rule && !l.verdict.reason?.includes(l.verdict.rule)
                  ? ` - ${rx('settings.rules.resultBy', { rule: l.verdict.rule })}`
                  : ''}
              </p>
            )}

            <p className="text-[11px] text-carbon-textSub">
              {names.length === 0
                ? rx('settings.rules.resultNone')
                : `${rx('settings.rules.resultMatched')}: ${names.join(' → ')}`}
            </p>

            {/* Shown for a link the filter rejects too: it is what the other
                list would do. */}
            {flavour === 'packagizer' && (
              <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-0.5 text-[11px]">
                <dt className="text-carbon-textMuted">{rx('settings.rules.resultPackage')}</dt>
                <dd dir="ltr" className="truncate text-carbon-text">
                  {l.result.package || '-'}
                </dd>
                <dt className="text-carbon-textMuted">{rx('settings.rules.resultFolder')}</dt>
                <dd dir="ltr" className="flex min-w-0 items-center truncate text-carbon-text">
                  {l.effect.dir ? (
                    l.effect.dir
                  ) : (
                    // No rule named a folder. The real folder is not computed
                    // here, since a second copy of that logic would drift.
                    <>
                      <span className="text-carbon-textMuted">
                        {downloadDir || rx('settings.rules.folderFromSettings')}
                      </span>
                      <InfoBubble tip={rx('settings.rules.folderFromSettingsHint')} />
                    </>
                  )}
                </dd>
                <dt className="text-carbon-textMuted">{rx('settings.rules.resultFilename')}</dt>
                <dd dir="ltr" className="truncate text-carbon-text">
                  {l.result.filename || '-'}
                </dd>
                {extras.length > 0 && (
                  <>
                    <dt className="text-carbon-textMuted">{rx('settings.rules.alsoSets')}</dt>
                    <dd className="truncate text-carbon-text">{extras.join(' · ')}</dd>
                  </>
                )}
              </dl>
            )}
          </li>
        );
      })}
      {report.disabled && <li className="text-[11px] text-carbon-textMuted">{rx('settings.rules.setOff')}</li>}
    </ul>
  );
}
