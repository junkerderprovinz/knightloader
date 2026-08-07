import { useRef, useState } from 'react';
import { uploadContainer } from '../lib/api';
import { fmtBytes } from '../lib/format';
import { useT } from '../lib/i18n';
import { Button, InfoBubble } from './ui';
import { IconFolder } from '../lib/icons';

// What the file dialog offers. The server decides the format from the bytes, not
// from the name — a .dlc saved as .txt by a browser is routine — so this is a
// convenience for the picker and never a gate: a file dragged in is sent
// whatever it is called, and the server's own sentence explains a refusal.
const CONTAINER_ACCEPT = '.txt,.dlc,.ccf,.rsdf';

// Mirrors container.MaxBytes in internal/container. Checked before the upload so
// a mis-picked ISO is refused here instead of being pushed up a domestic uplink
// to be refused with a 413 at the far end.
const MAX_CONTAINER_BYTES = 8 << 20;

/** One file's answer, kept structured so the sentence is built at render time
 *  and follows a language change instead of freezing at upload time. */
type Outcome =
  | { file: string; kind: 'staged'; links: number; created: number; pkg: string }
  | { file: string; kind: 'handed'; expiresIn: number }
  | { file: string; kind: 'failed'; reason: string };

function Result({ o }: { o: Outcome }) {
  const { t } = useT();

  if (o.kind === 'failed') {
    // The server's own words, framed rather than replaced: "only the headless
    // JDownloader backend can open it; none is configured" is an instruction,
    // and "upload failed" is not.
    return (
      <p className="px-4 text-xs text-statusFail">
        {t('container.failed', { file: o.file, reason: o.reason })}
      </p>
    );
  }

  // Nothing is staged yet in this case, and saying "0 links added" is what makes
  // people upload the same file four times. The links arrive over the websocket
  // when the backend has fetched the handover.
  if (o.kind === 'handed') {
    return (
      <p className="px-4 text-xs text-carbon-textSub">
        {t('container.handed', { file: o.file, n: o.expiresIn })}
      </p>
    );
  }

  // The container held links and none of them became a task: every one was
  // already in the list. Not a fault, and not silence either — the skipped strip
  // below carries the per-link reason.
  if (o.created === 0) {
    return (
      <p className="px-4 text-xs text-carbon-textSub">
        {t('container.allKnown', { file: o.file, n: o.links })}
      </p>
    );
  }

  const known = o.links - o.created;
  return (
    <p className="px-4 text-xs text-statusOk">
      {o.pkg
        ? t('container.stagedIn', { n: o.created, file: o.file, pkg: o.pkg })
        : t('container.staged', { n: o.created, file: o.file })}
      {known > 0 && ` ${t('container.alsoKnown', { n: known })}`}
    </p>
  );
}

/**
 * ContainerDrop takes the .txt/.dlc/.ccf/.rsdf files people are handed instead
 * of links.
 *
 * One row tall on purpose. It sits under the paste box, which is why anyone
 * opens this page, and an intake surface that pushes that box off the top has
 * cost more than it added.
 */
export function ContainerDrop({ pkg = '' }: { pkg?: string }) {
  const { t } = useT();
  const input = useRef<HTMLInputElement>(null);
  const [dragOver, setDragOver] = useState(false);
  const [busy, setBusy] = useState(false);
  const [results, setResults] = useState<Outcome[]>([]);

  async function send(files: File[]) {
    // A second batch is refused while one is in flight rather than queued: the
    // results block reports one batch, and interleaving two would leave a
    // "12 links staged" line standing next to a file it did not come from.
    if (!files.length || busy) return;
    setBusy(true);
    setResults([]);
    const out: Outcome[] = [];
    // Sequential, not Promise.all: each upload stages its links through the same
    // duplicate check, so two in flight make "8 of 12 were already known" depend
    // on which one happened to finish first.
    for (const f of files) {
      if (f.size > MAX_CONTAINER_BYTES) {
        out.push({
          file: f.name,
          kind: 'failed',
          reason: t('container.tooBig', { max: fmtBytes(MAX_CONTAINER_BYTES) }),
        });
      } else {
        try {
          const r = await uploadContainer(f, pkg);
          if (r.handedTo === 'jd') {
            out.push({ file: f.name, kind: 'handed', expiresIn: r.expiresIn });
          } else {
            // The package is read off what was actually created rather than off
            // the package field, because a Packagizer rule may have overridden
            // it; several packages means there is no single one to name.
            const landed = new Set(r.created.map((c) => c.package).filter(Boolean));
            out.push({
              file: f.name,
              kind: 'staged',
              links: r.links,
              created: r.created.length,
              pkg: landed.size === 1 ? [...landed][0] : '',
            });
          }
        } catch (e) {
          out.push({ file: f.name, kind: 'failed', reason: (e as Error).message });
        }
      }
      setResults([...out]); // each file reports as it lands, not after the last one
    }
    setBusy(false);
  }

  return (
    <div className="flex flex-col gap-1.5">
      <div
        onDragOver={(e) => {
          e.preventDefault();
          setDragOver(true);
        }}
        onDragLeave={() => setDragOver(false)}
        onDrop={(e) => {
          e.preventDefault();
          setDragOver(false);
          send([...e.dataTransfer.files]);
        }}
        className={`flex flex-wrap items-center gap-3 rounded-[var(--radius-control)] px-4 py-2 transition-colors ${
          dragOver ? 'bg-accentSoft shadow-[0_0_0_2px_var(--focus-ring)]' : 'bg-carbon-surface2'
        }`}
      >
        <span className="flex items-center text-xs text-carbon-textSub">
          {t('container.prompt')}
          <InfoBubble tip={t('container.info')} />
        </span>
        <span className="flex-1" />
        <Button
          kind="ghost"
          className="px-2.5 text-xs"
          icon={<IconFolder width={14} height={14} />}
          onClick={() => input.current?.click()}
          disabled={busy}
        >
          {busy ? t('container.uploading') : t('container.choose')}
        </Button>
        <input
          ref={input}
          type="file"
          hidden
          multiple
          accept={CONTAINER_ACCEPT}
          onChange={(e) => {
            send([...(e.target.files ?? [])]);
            // Cleared so picking the same file again still fires a change event.
            // Without this, a container re-uploaded after fixing the JD backend
            // silently does nothing.
            e.target.value = '';
          }}
        />
      </div>
      {results.map((o, i) => (
        <Result key={`${o.file}|${i}`} o={o} />
      ))}
    </div>
  );
}
