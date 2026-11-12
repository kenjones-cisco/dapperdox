const fs = require('node:fs/promises');
const path = require('node:path');
const os = require('node:os');
const { build } = require('esbuild');

async function main() {
  const check = process.argv.includes('--check');
  const destination = path.resolve('assets/static/browser');
  const output = await fs.mkdtemp(path.join(os.tmpdir(), 'dapperdox-assets-'));
  const packages = ['jquery', 'bootstrap', '@popperjs/core', 'highlight.js', '@fontsource/open-sans', '@fortawesome/fontawesome-free'];
  const root = name => {
    const directory = require.resolve.paths(name).map(base => path.join(base, name))
      .find(base => require('node:fs').existsSync(path.join(base, 'package.json')));
    if (!directory) throw new Error(`Missing browser dependency: ${name}`);
    return directory;
  };
  const copy = async (source, target) => {
    await fs.mkdir(path.dirname(target), { recursive: true });
    await fs.copyFile(source, target);
  };
  try {
    await build({
      entryPoints: ['scripts/browser.js'], bundle: true, minify: true,
      outfile: path.join(output, 'browser.js'), target: ['es2022'], legalComments: 'eof',
      nodePaths: [path.dirname(root('bootstrap'))],
    });
    await copy(path.join(root('bootstrap'), 'dist/css/bootstrap.min.css'), path.join(output, 'bootstrap.css'));
    await copy(path.join(root('highlight.js'), 'styles/xcode.css'), path.join(output, 'highlight.css'));
    const font = root('@fontsource/open-sans');
    let fontCSS = '';
    for (const weight of [400, 600]) {
      fontCSS += await fs.readFile(path.join(font, `${weight}.css`), 'utf8');
    }
    await fs.writeFile(path.join(output, 'fonts.css'), fontCSS);
    for (const file of (await fs.readdir(path.join(font, 'files'))).filter(name => /-(400|600)-normal\.woff2?$/.test(name))) {
      await copy(path.join(font, 'files', file), path.join(output, 'files', file));
    }
    const icons = root('@fortawesome/fontawesome-free');
    for (const file of ['fontawesome', 'solid', 'regular', 'brands', 'v4-shims']) {
      await copy(path.join(icons, 'css', `${file}.min.css`), path.join(output, 'css', `${file}.css`));
    }
    for (const file of await fs.readdir(path.join(icons, 'webfonts'))) {
      await copy(path.join(icons, 'webfonts', file), path.join(output, 'webfonts', file));
    }
    const versions = {};
    for (const name of packages) {
      const directory = root(name);
      const metadata = JSON.parse(await fs.readFile(path.join(directory, 'package.json'), 'utf8'));
      versions[name] = { version: metadata.version, license: metadata.license };
      const license = (await fs.readdir(directory)).find(file => /^licen[cs]e(\.|$)/i.test(file));
      if (!license) throw new Error(`Missing license for ${name}`);
      await copy(path.join(directory, license), path.join(output, 'licenses', `${name.replaceAll('/', '-')}.license`));
    }
    await fs.writeFile(path.join(output, 'packages.json'), `${JSON.stringify(versions, null, 2)}\n`);
    const files = async directory => (await fs.readdir(directory, { recursive: true, withFileTypes: true }))
      .filter(entry => entry.isFile()).map(entry => path.relative(directory, path.join(entry.parentPath, entry.name))).sort();
    const generated = await files(output);
    if (check) {
      const existing = await files(destination);
      if (JSON.stringify(generated) !== JSON.stringify(existing)) throw new Error('Generated asset file list differs; run npm run build:assets in Docker');
      for (const file of generated) {
        if (!(await fs.readFile(path.join(output, file))).equals(await fs.readFile(path.join(destination, file)))) {
          throw new Error(`Generated asset differs: ${file}; run npm run build:assets in Docker`);
        }
      }
    } else {
      try {
        for (const file of await files(destination)) {
          if (!generated.includes(file)) await fs.unlink(path.join(destination, file));
        }
      } catch (error) {
        if (error.code !== 'ENOENT') throw error;
      }
      for (const file of generated) await copy(path.join(output, file), path.join(destination, file));
    }
  } finally {
    await fs.rm(output, { recursive: true, force: true });
  }
}

main().catch(error => { console.error(error); process.exitCode = 1; });
