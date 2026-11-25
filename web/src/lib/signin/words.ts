// The words GlimStone's sign-in parts take, out of KnightLoader's language
// files, and the codes the kit answers with, each with a sentence of its own.
import {
  componentText,
  listOf,
  SIGN_IN_ERRORS,
  SIGN_IN_REDIRECTS,
  signInTileTextEn,
  type Plural,
  type SignInErrorText,
  type SignInReauthText,
  type SignInRemoved,
  type SignInText,
  type SignInTileText,
} from '@glimstone/core';
import type { TranslationKey } from '../i18n';
import { PRESET_LABEL } from '../../pages/settings/access/tokenScopes';

export type Translate = (key: TranslationKey, vars?: Record<string, string | number>) => string;

/** The scopes a new token takes, narrowest first, each standing for a preset of KnightLoader's rights. */
export const TOKEN_LEVELS = ['status', 'read', 'add', 'admin'] as const;

const LEVEL_PRESET = { status: 'status', read: 'read', add: 'addRead', admin: 'full' } as const;

export const ERROR_CODES = [...SIGN_IN_ERRORS, 'offline', 'cancelled'] as const;
export const ARRIVED_CODES = SIGN_IN_REDIRECTS;

const key = (k: string) => ('signin.' + k) as TranslationKey;

/**
 * plural turns one of KnightLoader's counted sentences, "{count|one:# key|other:# keys}",
 * into the forms GlimStone fills in itself.
 */
export function plural(raw: string): Plural {
  const inner = /^\{count\|(.*)\}$/s.exec(raw)?.[1];
  if (!inner) return { other: raw };
  const forms: Record<string, string> = {};
  for (const part of inner.split('|')) {
    const at = part.indexOf(':');
    forms[part.slice(0, at)] = part.slice(at + 1).replaceAll('#', '{count}');
  }
  return { other: '', ...forms } as Plural;
}

export function words(t: Translate, lang: string) {
  const s = (k: string) => t(key(k));
  const tile = (k: string) => s('tile.' + k);
  const counted = (k: string) => plural(tile(k));
  const fillCount = (p: Plural, n: number) => {
    const form = new Intl.PluralRules(lang).select(n) as keyof Plural;
    return (p[form] ?? p.other).replaceAll('{count}', String(n));
  };
  const field = componentText(lang).passwordField;
  const eye = { show: t('common.showPassword'), hide: t('common.hidePassword') };

  const errors = (): SignInErrorText => Object.fromEntries(ERROR_CODES.map((code) => [code, s('errors.' + code)]));
  const arrived = (path: string): Record<string, string> =>
    Object.fromEntries(ARRIVED_CODES.map((code) => [code, code === 'addressMissing' ? t(key('arrived.addressMissing'), { path }) : s('arrived.' + code)]));

  function card(): SignInText {
    return {
      name: s('name'),
      password: s('password'),
      signIn: s('signIn'),
      signingIn: s('signingIn'),
      passkey: '',
      forgot: s('forgot'),
      code: s('codeLabel'),
      codeHint: s('codeTip'),
      codeInvalid: s('badCode'),
      back: s('back'),
      resetCode: s('logCode'),
      resetCodeTip: s('logCodeTip'),
      newPassword: s('newPassword'),
      confirmPassword: field.confirm,
      setPassword: s('setPassword'),
      saving: s('saving'),
      mismatch: field.mismatch,
      tooShort: '',
      shortHint: s('shortHint'),
      resetRejected: s('resetRejected'),
      failed: s('failed'),
      ...eye,
    };
  }

  /** What the page after a reset says: the sessions that ended and whatever else the reset took away. */
  function resetNote(removed: SignInRemoved): string {
    const gone = [
      ...(removed.twoFactor ? [tile('password.goes.twoFactor')] : []),
      ...(removed.passkeys ? [fillCount(counted('password.goes.passkeys'), removed.passkeys)] : []),
      ...(removed.tokens ? [fillCount(counted('password.goes.tokens'), removed.tokens)] : []),
    ];
    return [
      s('resetDone'),
      ...(gone.length ? [t(key('resetGone'), { gone: listOf(lang, gone) })] : []),
      ...(removed.services ? [s('resetServices')] : []),
    ].join(' ');
  }

  function reauth(): SignInReauthText {
    return {
      title: s('reauth.title'),
      text: s('reauth.text'),
      password: s('password'),
      confirm: s('reauth.confirm'),
      cancel: t('common.cancel'),
      close: t('common.close'),
      ...eye,
      wrong: s('errors.wrongPassword'),
      off: s('errors.confirmOff'),
      failed: s('failed'),
    };
  }

  /** path is where the app's own address is set, in the page's words. */
  function tileText(path: string): SignInTileText {
    return {
      cancel: t('common.cancel'),
      close: t('common.close'),
      confirm: tile('confirm'),
      copy: t('common.copy'),
      copied: t('common.copied'),
      back: s('back'),
      save: t('settings.save'),
      remove: t('accounts.remove'),
      ...eye,
      confirmPassword: field.confirm,
      mismatch: field.mismatch,
      failed: s('failed'),
      needsPassword: { title: tile('needsPassword.title'), reason: tile('needsPassword.reason') },
      notLocal: { title: tile('notLocal.title'), reason: tile('notLocal.reason') },
      password: {
        title: s('password'),
        hint: tile('password.hint'),
        openHint: tile('password.openHint'),
        viaService: tile('password.viaService'),
        state: s('password'),
        set: tile('password.set'),
        none: tile('password.none'),
        noneHint: tile('password.noneHint'),
        change: tile('password.change'),
        add: tile('password.add'),
        current: tile('password.current'),
        next: s('newPassword'),
        save: tile('password.save'),
        saved: tile('password.saved'),
        first: tile('password.first'),
        wrongCurrent: tile('password.wrongCurrent'),
        shortHint: s('shortHint'),
        shortSaved: s('longer'),
        remove: tile('password.remove'),
        removeTitle: tile('password.removeTitle'),
        removeText: tile('password.removeText'),
        goes: {
          twoFactor: tile('password.goes.twoFactor'),
          passkeys: counted('password.goes.passkeys'),
          services: tile('password.goes.services'),
          tokens: counted('password.goes.tokens'),
        },
        removed: tile('password.removed'),
      },
      askPassword: {
        title: tile('askPassword.title'),
        hint: tile('askPassword.hint'),
        label: tile('askPassword.label'),
        when: tile('askPassword.when'),
        on: tile('askPassword.on'),
        off: tile('askPassword.off'),
      },
      twoFactor: {
        title: tile('twoFactor.title'),
        hint: tile('twoFactor.hint'),
        state: tile('twoFactor.state'),
        on: tile('twoFactor.on'),
        none: tile('twoFactor.none'),
        noneHint: tile('twoFactor.noneHint'),
        setUp: tile('twoFactor.setUp'),
        codesLeft: tile('twoFactor.codes'),
        codesLeftValue: counted('twoFactor.codesLeft'),
        newCodes: tile('twoFactor.newCodes'),
        turnOff: tile('twoFactor.turnOff'),
        setupTitle: tile('twoFactor.setupTitle'),
        scan: tile('twoFactor.scan'),
        qr: tile('twoFactor.qr'),
        key: tile('twoFactor.key'),
        code: tile('twoFactor.code'),
        confirm: tile('twoFactor.turnOn'),
        sixDigits: tile('twoFactor.sixDigits'),
        codesTitle: tile('twoFactor.codes'),
        codesText: tile('twoFactor.codesText'),
        copyCodes: tile('twoFactor.copyCodes'),
        codesSaved: tile('twoFactor.codesSaved'),
        turnedOn: tile('twoFactor.turnedOn'),
        newCodesReady: tile('twoFactor.newCodesReady'),
        offTitle: tile('twoFactor.offTitle'),
        offText: tile('twoFactor.offText'),
        offConfirm: tile('twoFactor.turnOff'),
        turnedOff: tile('twoFactor.turnedOff'),
      },
      passkeys: {
        title: tile('passkeys.title'),
        hint: tile('passkeys.hint'),
        add: tile('passkeys.add'),
        none: tile('passkeys.none'),
        noneHint: tile('passkeys.noneHint'),
        created: tile('passkeys.created'),
        used: tile('passkeys.used'),
        never: tile('passkeys.never'),
        here: tile('passkeys.here'),
        other: tile('passkeys.other'),
        synced: tile('passkeys.synced'),
        rename: tile('passkeys.rename'),
        renameTitle: tile('passkeys.renameTitle'),
        removeText: tile('passkeys.removeText'),
        removed: tile('passkeys.removed'),
        added: tile('passkeys.added'),
        renamed: tile('passkeys.renamed'),
        addTitle: tile('passkeys.add'),
        name: s('name'),
        nameHint: tile('passkeys.nameHint'),
        fallbackName: tile('passkeys.fallbackName'),
        continue: tile('passkeys.continue'),
        unavailableTitle: tile('passkeys.unavailableTitle'),
        unavailable: tile('passkeys.unavailable'),
      },
      sessions: {
        title: tile('sessions.title'),
        hint: tile('sessions.hint'),
        current: tile('sessions.current'),
        end: s('logout'),
        endOthers: tile('sessions.endOthers'),
        ended: tile('sessions.ended'),
        othersEnded: tile('sessions.othersEnded'),
        from: tile('sessions.from'),
        seen: tile('sessions.seen'),
        on: tile('sessions.on'),
        browser: tile('sessions.browser'),
      },
      tokens: {
        title: tile('tokens.title'),
        hint: tile('tokens.hint'),
        create: tile('tokens.create'),
        none: tile('tokens.none'),
        noneHint: tile('tokens.noneHint'),
        scopes: Object.fromEntries(TOKEN_LEVELS.map((l) => [l, t(PRESET_LABEL[LEVEL_PRESET[l]])])),
        created: tile('tokens.created'),
        used: tile('tokens.used'),
        neverUsed: tile('tokens.neverUsed'),
        expires: tile('tokens.expires'),
        forever: tile('tokens.forever'),
        endsWhenClosed: tile('tokens.endsWhenClosed'),
        openHint: tile('tokens.openHint'),
        revoke: tile('tokens.revoke'),
        revokeText: tile('tokens.revokeText'),
        revoked: tile('tokens.revoked'),
        newTitle: tile('tokens.create'),
        name: s('name'),
        nameHint: tile('tokens.nameHint'),
        scope: tile('tokens.scope'),
        lifetime: tile('tokens.lifetime'),
        lifetimes: {
          '30': fillCount(counted('tokens.days'), 30),
          '90': fillCount(counted('tokens.days'), 90),
          '365': tile('tokens.year'),
          never: tile('tokens.never'),
        },
        make: tile('tokens.make'),
        nameMissing: tile('tokens.nameMissing'),
        shownTitle: tile('tokens.shownTitle'),
        shownText: tile('tokens.shownText'),
        shownDone: tile('tokens.shownDone'),
      },
      services: {
        title: tile('services.title'),
        unnamed: tile('services.unnamed'),
        hint: tile('services.hint'),
        add: tile('services.add'),
        on: tile('twoFactor.on'),
        off: tile('twoFactor.off'),
        untested: tile('services.untested'),
        paused: tile('services.paused'),
        show: tile('services.show'),
        mustTest: tile('services.mustTest'),
        test: tile('services.test'),
        testing: tile('services.testing'),
        testOk: tile('services.testOk'),
        testFailed: tile('services.testFailed'),
        tested: tile('services.tested'),
        // KnightLoader sets up no service of its own, so these never show.
        fromConfig: signInTileTextEn.services.fromConfig,
        confirmAgain: signInTileTextEn.services.confirmAgain,
        edit: tile('services.edit'),
        openBinds: tile('services.openBinds'),
        removeText: tile('services.removeText'),
        removed: tile('services.removed'),
        turnedOn: tile('services.turnedOn'),
        turnedOff: tile('services.turnedOff'),
        updated: tile('services.updated'),
        pick: tile('services.add'),
        pickHint: tile('services.pickHint'),
        group: tile('services.group'),
        own: tile('services.own'),
        ownSub: 'OpenID Connect',
        ownHint: tile('services.ownHint'),
        search: tile('services.search'),
        searchNone: tile('services.searchNone'),
        clear: tile('services.clear'),
        saved: tile('services.saved'),
        formHint: tile('services.formHint'),
        whoGetsIn: tile('services.whoGetsIn'),
        role: tile('services.role'),
        roleHint: tile('services.roleHint'),
        issuer: tile('services.issuer'),
        issuerHint: tile('services.issuerHint'),
        clientId: tile('services.clientId'),
        secret: tile('services.secret'),
        secretKept: tile('services.secretKept'),
        redirect: tile('services.redirect'),
        redirectHint: tile('services.redirectHint'),
        label: tile('services.label'),
        scopes: tile('services.scopes'),
        nameClaim: tile('services.nameClaim'),
        groupsClaim: tile('services.groupsClaim'),
        more: tile('services.more'),
        turnOn: tile('twoFactor.turnOn'),
        missing: tile('services.missing'),
        unreachable: tile('services.unreachable'),
      },
      // An owner alone signs in with a password, so the tile never draws the accounts.
      accounts: signInTileTextEn.accounts,
      roles: signInTileTextEn.roles,
      via: {
        open: tile('via.open'),
        password: s('password'),
        passkey: tile('via.passkey'),
        service: '{name}',
        token: tile('via.token'),
        door: tile('via.door'),
      },
      errors: errors(),
      arrived: arrived(path),
    };
  }

  return { card, reauth, tileText, resetNote, errors, arrived };
}
