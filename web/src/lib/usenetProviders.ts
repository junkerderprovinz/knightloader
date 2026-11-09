// The Usenet providers the accounts page offers as tiles. Each server and port
// is the one the provider's own setup page gives for an encrypted connection,
// linked beside it. `connections` is the most that every plan of the provider
// allows, so the default never asks for more than an account takes; where the
// limit depends on the plan or the server and the provider does not say, it
// is left out and the form keeps its own default.

export interface UsenetProvider {
  id: string;
  name: string;
  /** The provider's website, which the tile's logo and second line come from. */
  site: string;
  host: string;
  port: number;
  connections?: number;
}

export const USENET_PROVIDERS: readonly UsenetProvider[] = [
  // https://help.easynews.com/kb/article/11-nntp-server-addresses/
  { id: 'easynews', name: 'Easynews', site: 'easynews.com', host: 'news.easynews.com', port: 563, connections: 60 },
  // https://help.eweka.nl/kb/article/730-all-about-tls-ssl
  // https://help.eweka.nl/kb/article/961-how-many-connections-does-my-account-come-with
  { id: 'eweka', name: 'Eweka', site: 'eweka.nl', host: 'news.eweka.nl', port: 563, connections: 50 },
  // https://frugalusenet.com/server-locations/
  { id: 'frugalusenet', name: 'Frugal Usenet', site: 'frugalusenet.com', host: 'news.frugalusenet.com', port: 563 },
  // https://support.giganews.com/hc/en-us/articles/33445852267277-NZBGet-Setup-Guide
  { id: 'giganews', name: 'Giganews', site: 'giganews.com', host: 'news.giganews.com', port: 563 },
  // https://support.newsgroup.ninja/kb/article/515-other-newsreaders/
  { id: 'newsgroupninja', name: 'Newsgroup Ninja', site: 'newsgroup.ninja', host: 'news.newsgroup.ninja', port: 563, connections: 50 },
  // https://support.newshosting.com/kb/article/104-newshosting-nntp-server-information/
  { id: 'newshosting', name: 'Newshosting', site: 'newshosting.com', host: 'news.newshosting.com', port: 563, connections: 30 },
  // http://www.thecubenet.com/clients/knowledgebase/1/What-are-the-server-addresses-and-ports.html
  { id: 'thecubenet', name: 'theCubeNet', site: 'thecubenet.com', host: 'news.thecubenet.com', port: 563 },
  // https://support.tweaknews.eu/kb/article/1803-grabit-setup-guide-for-tweaknews
  // https://www.tweaknews.eu/en/usenet-plans
  { id: 'tweaknews', name: 'Tweaknews', site: 'tweaknews.eu', host: 'news.tweaknews.eu', port: 563, connections: 30 },
  // https://support.usenetserver.com/kb/article/298-server-and-ports/
  // https://support.usenetserver.com/kb/article/188-i-received-a-502-maximum-connections-exceeded-error-how-many-connections-are-allowed
  { id: 'usenetserver', name: 'UsenetServer', site: 'usenetserver.com', host: 'news.usenetserver.com', port: 563, connections: 20 },
  // https://www.xsnews.com/faq.html
  { id: 'xsnews', name: 'XSNews', site: 'xsnews.nl', host: 'reader.xsnews.nl', port: 563 },
];
