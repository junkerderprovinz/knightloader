# Where files land

The download folder may be a plain path or a template:

```
/downloads/<jd:date>/<jd:hoster>/<jd:packagename>
```

Available: `<jd:packagename>`, `<jd:hoster>`, `<jd:filename>`, `<jd:date>`,
`<jd:year>`, `<jd:month>`, `<jd:day>` and `<jd:simpledate:FORMAT>` with the
usual `yyyy MM dd HH mm ss SSS` pattern letters. A task can also carry its own
folder, which always wins, and a Packagizer rule can set one per link, which is
how one paste lands in several places.

## Two variables that differ from JDownloader

The variables menu says so next to each. `<jd:source:N>` here is the **Nth path
segment of the source URL**, which is what a rule with no regular expression in
it can use. JDownloader's meaning, capture group N, is `<jd:match:FIELD:N>`,
which reads a group from any field the rule matched on, not only the source. A
rule naming a group on a field it has no `matches` condition for is refused when
you save it, rather than quietly producing the wrong folder.

## Telling a media library to rescan

Once the last file of a package has arrived **and been moved into its folder**,
one stored address can be called: a media library told to rescan. It is set up
on the Automation page (an address, GET or POST, and one header whose value is
sealed in the same encrypted store as your account logins) and switched on per
category, so only the drawers you pick call anything. Nothing is called until
you do both.
