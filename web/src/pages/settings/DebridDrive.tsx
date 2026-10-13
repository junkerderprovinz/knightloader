// The Debrid-Laufwerk page: the drive serves what is on the debrid accounts to
// other programs and sets up none of the accounts, so it has a page of its own.
import { DriveCard } from './debriddrive/DriveCard';

export function DebridDrive() {
  return (
    <div className="flex flex-col gap-10">
      <DriveCard hue={0} />
    </div>
  );
}
