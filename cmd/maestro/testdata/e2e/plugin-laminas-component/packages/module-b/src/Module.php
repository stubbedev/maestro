<?php

namespace Local\ModuleB;

class Module
{
    public function getModuleDependencies()
    {
        return ['Local\ComponentA'];
    }
}
