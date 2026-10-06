#!/bin/sh
# Rebuilds the par2 sets here with par2cmdline 1.4.0. The data files are
# random, so a rebuild changes every file and the tests take the new sets as
# they come. release/ has volumes of one size, posted/ the doubling sizes most
# posts use, and zipped/ protects an archive that can be unpacked.
set -eu
cd "$(dirname "$0")"
rm -rf release posted zipped
mkdir release posted zipped

cd release
head -c 150001 /dev/urandom > movie.mkv
head -c 9000 /dev/urandom > subs.srt
head -c 100 /dev/urandom > tiny.nfo
par2 create -q -s4096 -c12 -n3 release.par2 movie.mkv subs.srt tiny.nfo

cd ../posted
head -c 400000 /dev/urandom > show.part1.rar
head -c 200003 /dev/urandom > show.part2.rar
par2 create -q -s8192 -c24 show.par2 show.part1.rar show.part2.rar

cd ../zipped
head -c 30000 /dev/urandom > episode.mkv
python3 -c 'import zipfile; z = zipfile.ZipFile("episode.zip", "w"); z.write("episode.mkv"); z.close()'
rm episode.mkv
par2 create -q -s4096 -c6 episode.par2 episode.zip
