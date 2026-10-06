<?php

/*
 * maestro's plugin shim: Composer\Autoload\ClassMapGenerator
 * (docs/PLUGINS.md §4.9), Composer's deprecated wrapper over the vendored
 * composer/class-map-generator, which does the scanning in PHP as in
 * Composer.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Autoload;

use Composer\ClassMapGenerator\FileList;

class ClassMapGenerator
{
    public static function createMap($path, ?string $excluded = null, ?\Composer\IO\IOInterface $io = null, ?string $namespace = null, ?string $autoloadType = null, array &$scannedFiles = []): array
    {
        $generator = new \Composer\ClassMapGenerator\ClassMapGenerator(['php', 'inc', 'hh']);
        $fileList = new FileList();
        $fileList->files = $scannedFiles;
        $generator->avoidDuplicateScans($fileList);

        $generator->scanPaths($path, $excluded, $autoloadType ?? 'classmap', $namespace);

        $classMap = $generator->getClassMap();

        $scannedFiles = $fileList->files;

        if ($io !== null) {
            foreach ($classMap->getPsrViolations() as $msg) {
                $io->writeError("<warning>$msg</warning>");
            }

            foreach ($classMap->getAmbiguousClasses() as $class => $paths) {
                if (count($paths) > 1) {
                    $io->writeError(
                        '<warning>Warning: Ambiguous class resolution, "'.$class.'"'.
                        ' was found '. (count($paths) + 1) .'x: in "'.$classMap->getClassPath($class).'" and "'. implode('", "', $paths) .'", the first will be used.</warning>'
                    );
                } else {
                    $io->writeError(
                        '<warning>Warning: Ambiguous class resolution, "'.$class.'"'.
                        ' was found in both "'.$classMap->getClassPath($class).'" and "'. implode('", "', $paths) .'", the first will be used.</warning>'
                    );
                }
            }
        }

        return $classMap->getMap();
    }

    public static function dump(iterable $dirs, string $file): void
    {
        $maps = [];

        foreach ($dirs as $dir) {
            $maps = array_merge($maps, static::createMap($dir));
        }

        file_put_contents($file, sprintf('<?php return %s;', var_export($maps, true)));
    }
}
