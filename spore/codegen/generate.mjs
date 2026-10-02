import { execFileSync } from 'node:child_process'
import { existsSync, readFileSync, readdirSync, rmSync, writeFileSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const codegen = dirname(fileURLToPath(import.meta.url))
const spore = resolve(codegen, '..')
const spec = resolve(spore, 'openapi.json')
const cli = resolve(codegen, 'node_modules/.bin/openapi-generator-cli')

const clients = [
  ['python', 'python'],
  ['php', 'php'],
  ['ruby', 'ruby'],
  ['rust', 'rust'],
]

const generatedScaffolding = {
  python: ['.github', '.gitlab-ci.yml', '.travis.yml', 'git_push.sh'],
  php: ['.travis.yml', 'git_push.sh'],
  ruby: ['.gitlab-ci.yml', '.travis.yml', 'git_push.sh'],
  rust: ['.travis.yml', 'git_push.sh'],
}

function replaceReadmeSection(output, heading, snippet) {
  const readme = resolve(output, 'README.md')
  const source = readFileSync(readme, 'utf8')
  const start = source.search(new RegExp(`^## ${heading}`, 'm'))
  const next = source.slice(start + 3).search(/^## /m)
  if (start < 0 || next < 0) {
    throw new Error(`no "${heading}" section to replace in ${readme}`)
  }
  const end = start + 3 + next
  writeFileSync(readme, source.slice(0, start) + readFileSync(snippet, 'utf8') + source.slice(end))
}

function widenGuzzleConstraints(output) {
  const path = resolve(output, 'composer.json')
  const composer = JSON.parse(readFileSync(path, 'utf8'))
  composer.require['guzzlehttp/guzzle'] = '^7.3 || ^8.0'
  composer.require['guzzlehttp/psr7'] = '^1.7 || ^2.0 || ^3.0'
  writeFileSync(path, `${JSON.stringify(composer, null, 4)}\n`)
}

function dropGuzzleJsonEncode(directory) {
  for (const entry of readdirSync(directory, { withFileTypes: true })) {
    const path = join(directory, entry.name)
    if (entry.isDirectory()) {
      dropGuzzleJsonEncode(path)
      continue
    }
    if (!entry.name.endsWith('.php')) continue
    const source = readFileSync(path, 'utf8')
    const rewritten = source.replace(
      /\\GuzzleHttp\\Utils::jsonEncode\((.*)\);$/gm,
      '\\json_encode($1, \\JSON_THROW_ON_ERROR);',
    )
    if (rewritten.includes('GuzzleHttp\\Utils::jsonEncode')) {
      throw new Error(`unrewritten GuzzleHttp\\Utils::jsonEncode left in ${path}`)
    }
    if (rewritten !== source) writeFileSync(path, rewritten)
  }
}

function normalizeGeneratedFiles(directory) {
  for (const entry of readdirSync(directory, { withFileTypes: true })) {
    const path = join(directory, entry.name)
    if (entry.isSymbolicLink() || ['dist', 'node_modules', 'target'].includes(entry.name)) {
      continue
    }
    if (entry.isDirectory()) {
      normalizeGeneratedFiles(path)
      continue
    }

    const source = readFileSync(path, 'utf8')
    const normalized = source.replace(/[\t ]+$/gm, '').replace(/\n+$/, '\n')
    if (normalized !== source) writeFileSync(path, normalized)
  }
}

normalizeGeneratedFiles(resolve(spore, 'sdk-node'))

for (const [name, generator] of clients) {
  const output = resolve(spore, `sdk-${name}`)
  if (!output.startsWith(`${spore}/sdk-`)) {
    throw new Error(`refusing to replace unexpected SDK path: ${output}`)
  }

  rmSync(output, { recursive: true, force: true })
  const args = [
    'generate',
    '-g',
    generator,
    '-i',
    spec,
    '-o',
    output,
    '-c',
    resolve(codegen, 'config', `${name}.json`),
    '--global-property',
    'apiDocs=false,modelDocs=false,apiTests=false,modelTests=false',
  ]
  if (name !== 'php') {
    args.push('--git-user-id', 'lalternativefabrique', '--git-repo-id', 'packages')
  }
  execFileSync(cli, args, { cwd: codegen, stdio: 'inherit' })
  for (const relativePath of generatedScaffolding[name]) {
    rmSync(resolve(output, relativePath), { recursive: true, force: true })
  }
  for (const [heading, folder] of [['Installation', 'install'], ['Getting Started', 'getting-started']]) {
    const snippet = resolve(codegen, folder, `${name}.md`)
    if (existsSync(snippet)) replaceReadmeSection(output, heading, snippet)
  }
  if (name === 'php') {
    widenGuzzleConstraints(output)
    dropGuzzleJsonEncode(resolve(output, 'lib'))
  }
  normalizeGeneratedFiles(output)
}
