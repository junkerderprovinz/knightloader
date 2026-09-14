import { useEffect, useState } from 'react';
import logoUrl from '../assets/logo.svg';
import { ApiError, fetchTasks, type Task } from '../lib/api';
import { fmtSpeed } from '../lib/format';
import { useT } from '../lib/i18n';
import { Card, Button, IconBadge, LabelBadge, useTooltip } from './ui';
import { IconTrash } from '../lib/icons';

interface Stats {
  online: boolean;
  /**
   * Reached, and it said no. Distinct from offline because the fix is
   * different: a peer refuses once the credential pairing gave it stops being
   * valid, which happens on its own the moment that peer sets or changes its
   * password - every token it issued is revoked with it. Shown as plain
   * offline, that reads as a machine somebody unplugged.
   */
  refused: boolean;
  active: number;
  total: number;
  speed: number;
}

// usePeerStats polls one instance for its live figures.
function usePeerStats(base: string): Stats | null {
  const [stats, setStats] = useState<Stats | null>(null);
  useEffect(() => {
    let alive = true;
    const load = async () => {
      try {
        const list: Task[] = await fetchTasks(base);
        if (!alive) return;
        const running = list.filter((x) => x.status === 'running' || x.status === 'extracting').length;
        const speed = list.reduce((s, x) => s + (x.status === 'running' ? x.speed : 0), 0);
        setStats({ online: true, refused: false, active: running, total: list.length, speed });
      } catch (e) {
        // "It refused us" is not "it is switched off", and the two need
        // opposite reactions. A peer stops accepting this instance whenever the
        // credential pairing handed over stops being valid - most easily by
        // that peer setting or changing its password, which revokes every token
        // it ever issued. Reported as plain offline, that looks like a machine
        // somebody unplugged, and the pairing that would fix it is the last
        // thing anyone would try.
        const refused = e instanceof ApiError && (e.status === 401 || e.status === 403);
        if (alive) setStats({ online: false, refused, active: 0, total: 0, speed: 0 });
      }
    };
    load();
    const iv = setInterval(load, 3000);
    return () => {
      alive = false;
      clearInterval(iv);
    };
  }, [base]);
  return stats;
}

// InstanceRow is the quiet form used where instances are a summary rather than
// the subject of the page: a dot, the name, and the current speed.
export function InstanceRow({ name, base, onOpen }: { name: string; base: string; onOpen?: () => void }) {
  const { t } = useT();
  const stats = usePeerStats(base);
  const online = stats?.online ?? false;
  const refused = stats?.refused ?? false;
  const state = online ? t('instances.online') : refused ? t('instances.refused') : t('instances.offline');

  // THE HOUSE BUBBLE, not a native title=. The dot carries the state in colour
  // alone and nothing beside it spells the word out, so it does owe a tooltip -
  // but the OS balloon is drawn in the OS font, at the pointer instead of at the
  // trigger, and by none of the rules every other bubble in this app follows.
  //
  // Only the hover half of the handle is spread. triggerProps also carries
  // tabIndex and role='note', and this row is a <button> whenever it can be
  // opened: a descendant with a tabindex is markup a browser cannot make sense
  // of inside one, the same reason ColumnMenu keeps its (i) outside its rows.
  // Nothing is lost by it - aria-label keeps the word in the row's own
  // accessible name, which is what a keyboard or screen reader arrives at.
  const tip = useTooltip<HTMLSpanElement>(state);
  const { ref: tipRef, onMouseEnter, onMouseLeave, 'aria-describedby': tipDescribedBy } = tip.triggerProps;

  const body = (
    <>
      <span
        ref={tipRef}
        onMouseEnter={onMouseEnter}
        onMouseLeave={onMouseLeave}
        aria-describedby={tipDescribedBy}
        role="img"
        aria-label={state}
        className={`h-2 w-2 shrink-0 rounded-[var(--radius-pill)] ${online ? 'bg-statusOkSolid' : 'bg-statusFailSolid'}`}
      />
      <span className="min-w-0 flex-1 truncate text-[14px] text-carbon-text">{name}</span>
      <span className="glim-num text-xs text-carbon-textSub">
        {stats ? fmtSpeed(stats.speed) || '—' : '—'}
      </span>
    </>
  );
  return (
    <>
      {onOpen ? (
        <button
          onClick={onOpen}
          className="flex w-full items-center gap-3 px-6 py-4 text-left transition-colors hover:bg-carbon-hover/50"
        >
          {body}
        </button>
      ) : (
        <div className="flex items-center gap-3 px-6 py-4">{body}</div>
      )}
      {tip.node}
    </>
  );
}

// One instance at a glance: a status dot, the host, and three quiet figures.
export function InstanceCard({
  name,
  url,
  relayId,
  base,
  onOpen,
  onRemove,
  hue,
  isSelf = false,
}: {
  /** The label shown - callers pass a peer's displayName (falling back to
   *  its name) here, never the raw relay address. */
  name: string;
  url: string;
  /** Set for a peer reached through the relay: url is empty for one of
   *  these (federation.Instance's own doc comment on why), so the second
   *  line shows "connected via relay" instead of a blank line. */
  relayId?: string;
  base: string;
  onOpen?: () => void;
  onRemove?: () => void;
  /** This card's position in the palette, handed to the CARD and not to one
   *  badge inside it (GlimStone: "the position belongs on the container, not
   *  on the one visible thing inside it"). `.glim-hue` rebinds --accent for
   *  the whole subtree, so one call here colours the status badge, the Open
   *  button, the remove badge and the focus ring at once - with the position
   *  on the remove badge alone, a card whose peer cannot be removed was not
   *  in the mode at all. Every card in the row is a member of the same
   *  equal-weight set, this instance's own included. */
  hue?: number;
  /** Marks the card for the instance you are looking at right now. It gets
   *  the same Open button as any peer (pointing at the local download list),
   *  so the row does not have one card shaped differently from the rest, plus
   *  a quiet label saying which one it is. */
  isSelf?: boolean;
}) {
  const { t } = useT();
  const stats = usePeerStats(base);
  const online = stats?.online ?? false;
  const refused = stats?.refused ?? false;
  // Three states, one badge. "Refused" is deliberately neither green nor red:
  // the peer is there and answering, it just will not accept this instance -
  // a different thing from a machine that is gone, and one a person fixes by
  // re-entering the phrase rather than by checking cables. It takes a rainbow
  // hue instead of a status tone, which is exactly the "neither ok nor
  // failed" the two status colours cannot express.
  const state = online ? t('instances.online') : refused ? t('instances.refused') : t('instances.offline');

  return (
    // padding="none" and a horizontal split, so the mark can sit flush
    // against the card's own left edge and run its full height (jdp,
    // 2026-08-27: "soll jede Instanz in deren card das logo ganz links in
    // der card sein und die card in der höhe ausfüllen"). With Card's usual
    // p-5 the mark would float inside a 20px margin instead, which is the
    // one thing that was asked against. overflow-hidden clips it to the
    // card's own corner radius; without it a square-edged image pokes out of
    // a rounded card at both left corners.
    //
    // The right-hand column carries the padding the card gave up, and it is
    // the side that decides the card's height - the mark is `self-stretch`
    // and so takes whatever height it is handed rather than setting it,
    // which is what keeps a row of cards the same height whatever their
    // contents.
    //
    // NO `hover` ON THE CARD, and it is the one call site in the app that ever
    // passed it. The prop puts `motion-safe:hover:-translate-y-0.5` on the
    // surface, which lifted this card two pixels under the pointer - measured
    // at 14px against its neighbours' 16px on a row of three, so the shared top
    // edge of the grid broke for as long as a pointer rested anywhere on it
    // (jdp: "wenn man auf die card hoovert wandert sie nach oben").
    //
    // Removed rather than made smaller, for three reasons, and the third is the
    // one that settles it. GlimStone's motion engine names the five things that
    // move and then says what does not: "a card doesn't breathe, hover states
    // change instantly". Rule 21 says hover moves up the SURFACE RAMP, which is
    // a tone and never a position, and a card's own surface has no rung on that
    // ramp because a card is not a control. And this card cannot be clicked:
    // clicking its body does nothing, verified live - the only click target it
    // has ever had is the Open button below, which takes its own correct
    // surface2 -> surface3 hover from Button. A lift is the gesture a whole-card
    // link makes, so on this card it was a promise nothing here could keep.
    //
    // Nothing is lost with it gone. The Open button still answers the pointer,
    // and `group` stays because the remove badge's `group-hover:opacity-100`
    // reveal is rule 6's, a secondary action appearing on hover - which is a
    // control arriving, not the surface moving.
    //
    // web/check-card-hover.mjs is the guard. The prop itself is now unused in
    // ui.tsx; removing it there is that file's business, not this one's.
    <Card padding="none" hue={hue} className="group relative flex h-full flex-col overflow-hidden">
      {/* DIE ZWEI SPALTEN SIND EINE ZEILE, und der Knopf darunter ist eine
          zweite. Vorher war der Knopf `absolute bottom-5` und die Textspalte
          hielt mit `pb-16` Platz fuer ihn frei - zwei Zahlen fuer eine Sache,
          die zusammenpassen mussten, ohne dass etwas sie zusammenhaelt. Wird der
          Knopf hoeher, waechst der Text oder aendert sich die Kartenhoehe, legt
          er sich ueber die Zeile darueber (jdp: "da ist der button zu weit oben
          und verdeckt sachen").
          Als echte Zeile kann das nicht mehr passieren: sie nimmt den Platz, den
          sie braucht, und der Rest der Karte weicht. Der frueher hier notierte
          Grund gegen eine dritte Zeile - die Karte sei zweispaltig, und eine
          Zeile ueber die ganze Breite sei keine der beiden Spalten - loest sich
          damit auf, denn die Spalten liegen jetzt in einer Zeile darueber. */}
      <div className="flex min-h-0 flex-1 items-stretch">
      {/* The mark, larger again and then larger once more (jdp, 2026-08-27,
          twice: "Das logo in den instanzencard bitte größer") but still on the
          card's own surface with no plate behind it, which was the other half
          of that earlier correction.
          THE THIRD STEP WENT PAST THE RAIL, and back to 7rem is where it lands.
          `h-36` drew the shield at 144px while the brand mark at the top of the
          sidebar - the one place the app states its own identity - draws it at
          `h-28`, 112px. The largest drawing of the app's mark anywhere in the
          app was a repeat of it on a card in a grid, 29% louder than the brand
          itself (jdp: "das logo ist ein kleines bischen zu gross"). 7rem is not
          a number picked to be smaller: it is the size the rail already uses,
          the only other size the app owns above 40px, and the one the sentence
          below this has claimed all along - the commit that bumped the class to
          `h-36` left the prose saying 7rem, so the file described the right
          size and rendered a different one for three releases.
          It buys the text column back its width, which is measurable rather
          than a matter of taste: the reserved right-hand column leaves about
          145px for three metric labels, and at 144px and at 128px "AUFGABEN"
          and "TEMPO" render touching, as one word. 112px is the first step at
          which they separate. web/check-mark-scale.mjs is the guard.
          `max-h-full` rather than a bare height: the right-hand column decides
          how tall the card is, and a mark taller than that column would start
          setting the height itself - which is the one thing the split here
          exists to prevent. It grows to 7rem where the card allows it and
          stops at the card's own edge where it does not. */}
      <div className="flex shrink-0 items-center self-stretch pl-4">
        <img src={logoUrl} alt="" aria-hidden className="h-28 max-h-full w-auto" />
      </div>

      {/* The state as a badge in the corner, the same shape the connection
          card uses (jdp, 2026-08-27: "Der status-punkt soll wie in der
          Fernzugriffcard ein badge in der rechten oberen ecke sein"). A 8px
          dot beside the name said the same thing in a form that had to be
          hovered to be read at all. Absolutely placed so it cannot push the
          name around, and the name row reserves room for it. */}
      <span className="absolute right-5 top-5 z-10">
        <LabelBadge label={state} tone={online ? 'ok' : refused ? undefined : 'fail'} hue={refused ? 3 : undefined} />
      </span>

      {/* Roomier, on request (jdp, 2026-09-07: "Im instanzentab kannst du die
          instanzencards größer und luftiger machen"). p-7 and gap-4 rather than
          p-5 and gap-3: an instance card is the one card on its page and it
          holds four short readings, so it was a dense little tile in a lot of
          empty page. The reserved right-hand column grows with it. */}
      <div className="flex min-w-0 flex-1 flex-col gap-4 p-7 pr-36">
        {/* Name and address are ONE block with a hairline gap, not two rows of
            the card's own gap-4 (jdp, 2026-09-07: "die IP näher unter den
            namen"). They answer one question together - which machine is this -
            and four pixels of air said they were two separate readings, the
            same weight as the counters below. */}
        <div className="flex flex-col gap-0.5">
        <div className="flex items-center gap-2.5">
          <span className="truncate font-semibold text-carbon-text">{name}</span>
          {/* Which card is the machine you are on. An eyebrow rather than a
              coloured pill: it is an orientation aid, not a status. */}
          {isSelf && <span className="glim-eyebrow shrink-0">{t('instances.thisInstance')}</span>}
          <span className="flex-1" />
          {onRemove && (
            <span className="opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100">
              {/* 16px in a 32px badge, not lib/icons.tsx's 22px base size.
                  GlimStone 1.8.0 rule 13: a glyph ALONE in a square is half its
                  box, because there are no words beside it to match and the
                  only proportion left is how much of the frame the ink fills -
                  22 in 32 is 69% and reads as chunky. The 20px a glyph takes is
                  the size it takes NEXT TO TEXT, and this badge was the one
                  place in the app still carrying the bare default.
                  `labelled` for the same reason every other badge in the app's
                  toolbars carries it: the Beschriftung setting decides whether
                  the words appear, not the call site. The label is `title`,
                  which is already here, so no catalogue gains a key. */}
              <IconBadge
                labelled
                hue={hue}
                icon={<IconTrash width={16} height={16} />}
                title={t('instances.removeTitle', { name })}
                aria-label={t('instances.removeTitle', { name })}
                onClick={onRemove}
              />
            </span>
          )}
        </div>

        <div className="truncate text-xs text-carbon-textMuted">{relayId ? t('instances.viaRelay') : url}</div>
        </div>

        <div className="flex items-baseline gap-7">
          <Metric value={stats?.active ?? '—'} label={t('instances.metricActive')} />
          <Metric value={stats?.total ?? '—'} label={t('instances.metricTasks')} />
          <Metric value={stats ? fmtSpeed(stats.speed) || '0' : '—'} label={t('instances.metricSpeed')} />
        </div>

      </div>

      </div>

      {/* Eine eigene Zeile ueber die ganze Kartenbreite (jdp, 2026-09-07: "Der
          oeffnen button weiter nach unten und soll bis ganz nach rechts gehen").
          Der Rand kommt hier an den Knopf statt an die Karte, weil die Karte
          selbst `padding="none"` traegt - das Logo soll sie ja bis an die Kante
          ausfuellen. */}
      {onOpen && (
        <Button kind="secondary" onClick={onOpen} className="mx-5 mb-5 justify-center">
          {t('instances.open')}
        </Button>
      )}
    </Card>
  );
}

function Metric({ value, label }: { value: React.ReactNode; label: string }) {
  return (
    <div className="min-w-0">
      <div className="glim-num text-sm font-semibold text-carbon-text">{value}</div>
      <div className="glim-eyebrow">{label}</div>
    </div>
  );
}
