# gclone

I like to store repositories in a specific directory, where I create subdirectories in the format of the full
repository address.

For example, the repository github.com/juev/gclone it will be cloned to the ~/src directory along the way:

```sh
~/src/github.com/juev/gclone 
```

In order not to manually create directories and then clone the repository, I created this small program that allows
you to do this with one command.

```sh
GIT_PROJECT_DIR="~/src" gclone https://github.com/juev/gclone.git 
```

## Install

Just:

```sh
go install github.com/juev/gclone@latest
```

## Usage

The location for repositories is determined using the environment variable `GIT_PROJECT_DIR`.

You can set it in the system. Or transfer it when the program starts. If the variable is not set, the current
directory will be used as the main one.

At the output of the program, we will receive either an error in Stderr, or the resulting directory into which
the repository was cloned.

We can use the output in a command like:

```sh
cd $(gclone $1)
```

### Fish shell

Personally I use these functions:

```fish
function gcd --argument repo
    if test "$argv[1]" = ""
        echo "argument is empty"
        return
    end
    cd $(gclone -q $argv[1])
end
```

```fish
function gcode --argument repo
    if test "$argv[1]" = ""
        echo "argument is empty"
        return
    end
    code $(gclone -q $argv[1])
end
```

Enjoy!
