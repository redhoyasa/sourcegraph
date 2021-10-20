import {
    ListboxOption,
    ListboxInput,
    ListboxButton,
    ListboxPopover,
    ListboxList,
    ListboxGroupLabel,
} from '@reach/listbox'
import classNames from 'classnames'
import CloseIcon from 'mdi-react/CloseIcon'
import FlagVariantIcon from 'mdi-react/FlagVariantIcon'
import HelpCircleOutlineIcon from 'mdi-react/HelpCircleOutlineIcon'
import MenuDownIcon from 'mdi-react/MenuDownIcon'
import React, { useCallback } from 'react'

import { VersionContextProps } from '@sourcegraph/shared/src/search/util'
import { useLocalStorage } from '@sourcegraph/shared/src/util/useLocalStorage'

import { VersionContext } from '../schema/site.schema'
import { SubmitSearchProps } from '../search/helpers'

import styles from './VersionContextDropdown.module.scss'

const HAS_DISMISSED_INFO_KEY = 'sg-has-dismissed-version-context-info'

export interface VersionContextDropdownProps
    extends VersionContextProps,
        Partial<Pick<SubmitSearchProps, 'submitSearch'>> {
    setVersionContext: (versionContext: string | undefined) => Promise<void>
    availableVersionContexts: VersionContext[] | undefined

    /**
     * Whether to always show the expanded state. Used for testing.
     */
    alwaysExpanded?: boolean
    portal?: boolean

    className?: string
}

export const VersionContextDropdown: React.FunctionComponent<VersionContextDropdownProps> = ({
    setVersionContext,
    availableVersionContexts,
    versionContext: currentVersionContext,
    alwaysExpanded,
    portal,
    className,
    submitSearch,
}) => {
    /** Whether the user has dismissed the info blurb in the dropdown. */
    const [hasDismissedInfo, setHasDismissedInfo] = useLocalStorage(HAS_DISMISSED_INFO_KEY, false)

    const submitOnToggle = useCallback(
        (versionContext?: string): void => {
            submitSearch?.({
                source: 'filter',
                versionContext,
                activation: undefined,
                searchParameters: [{ key: 'from-context-toggle', value: 'true' }],
            })
        },
        [submitSearch]
    )

    const updateValue = useCallback(
        (newValue?: string): void => {
            setVersionContext(newValue).catch(error => {
                console.error('Error sending initial versionContext to extensions', error)
            })
            submitOnToggle(newValue)
        },
        [setVersionContext, submitOnToggle]
    )

    const disableValue = useCallback((): void => {
        updateValue(undefined)
    }, [updateValue])

    if (!availableVersionContexts || availableVersionContexts.length === 0) {
        return null
    }

    const onDismissInfo = (event: React.MouseEvent<HTMLButtonElement>): void => {
        event.preventDefault()
        setHasDismissedInfo(true)
    }

    const showInfo = (event: React.MouseEvent<HTMLButtonElement>): void => {
        event.preventDefault()
        setHasDismissedInfo(false)
    }

    return (
        <>
            {availableVersionContexts ? (
                <div className={classNames('text-nowrap', styles.versionContextDropdown, className)}>
                    <ListboxInput value={currentVersionContext} onChange={updateValue}>
                        {({ isExpanded }) => (
                            <>
                                <ListboxButton className={classNames('btn btn-secondary', styles.button)}>
                                    <FlagVariantIcon className="icon-inline small" />
                                    {!currentVersionContext || currentVersionContext === 'default' ? (
                                        <span
                                            className={classNames(
                                                'ml-2 mr-1',
                                                styles.buttonText,
                                                // If the info blurb hasn't been dismissed, still show the label on non-small screens.
                                                { 'd-sm-none d-md-block': !hasDismissedInfo },
                                                // If the info blurb has been dismissed, never show this label.
                                                { 'd-none': hasDismissedInfo }
                                            )}
                                        >
                                            Select context
                                        </span>
                                    ) : (
                                        <span className={classNames('ml-2 mr-1', styles.buttonText)}>
                                            {currentVersionContext} (Active)
                                        </span>
                                    )}
                                    <MenuDownIcon className="icon-inline" />
                                </ListboxButton>
                                <ListboxPopover
                                    className={classNames('dropdown-menu', styles.popover, {
                                        show: isExpanded || alwaysExpanded,
                                    })}
                                    portal={portal}
                                >
                                    {hasDismissedInfo && (
                                        <div className={classNames('pl-2 mb-1', styles.title)}>
                                            <span className="text-nowrap">Select version context</span>
                                            <button type="button" className="btn btn-icon" onClick={showInfo}>
                                                <HelpCircleOutlineIcon className="icon-inline small" />
                                            </button>
                                        </div>
                                    )}
                                    {!hasDismissedInfo && (
                                        <div className={classNames('card', styles.info)}>
                                            <span className="font-weight-bold">About version contexts</span>
                                            <p className="mb-2">
                                                Version contexts (
                                                <a href="http://docs.sourcegraph.com/user/search#version-contexts">
                                                    documentation
                                                </a>
                                                ) allow you to search a set of repositories based on a commit hash, tag,
                                                or other interesting moment in time of multiple code bases. Your
                                                administrator can configure version contexts in the site configuration.
                                            </p>
                                            <button
                                                type="button"
                                                className={classNames('btn btn-outline-primary', styles.infoDismiss)}
                                                onClick={onDismissInfo}
                                            >
                                                Do not show this again
                                            </button>
                                        </div>
                                    )}
                                    <ListboxList className={styles.list}>
                                        <ListboxGroupLabel
                                            disabled={true}
                                            value="title"
                                            className={classNames(styles.option, styles.title)}
                                        >
                                            <VersionContextInfoRow
                                                name="Name"
                                                description="Description"
                                                isActive={false}
                                                onDisableValue={disableValue}
                                            />
                                        </ListboxGroupLabel>
                                        {availableVersionContexts
                                            // Render the current version context at the top, then other available version
                                            // contexts in alphabetical order.
                                            ?.sort((a, b) => {
                                                if (a.name === currentVersionContext) {
                                                    return -1
                                                }
                                                if (b.name === currentVersionContext) {
                                                    return 1
                                                }
                                                return a.name > b.name ? 1 : -1
                                            })
                                            .map(versionContext => (
                                                <ListboxOption
                                                    key={versionContext.name}
                                                    value={versionContext.name}
                                                    label={versionContext.name}
                                                    className={styles.option}
                                                >
                                                    <VersionContextInfoRow
                                                        name={versionContext.name}
                                                        description={versionContext.description || ''}
                                                        isActive={currentVersionContext === versionContext.name}
                                                        onDisableValue={disableValue}
                                                    />
                                                </ListboxOption>
                                            ))}
                                    </ListboxList>
                                </ListboxPopover>
                            </>
                        )}
                    </ListboxInput>
                </div>
            ) : null}
        </>
    )
}

const VersionContextInfoRow: React.FunctionComponent<{
    name: string
    description: string
    isActive: boolean
    onDisableValue: () => void
}> = ({ name, description, isActive, onDisableValue }) => (
    <>
        <div>
            {isActive && (
                <button
                    type="button"
                    className="btn btn-icon"
                    onClick={onDisableValue}
                    aria-label="Disable version context"
                >
                    <CloseIcon className="icon-inline small" />
                </button>
            )}
        </div>
        <span className={styles.optionName}>{name}</span>
        <span className={styles.optionDescription}>{description}</span>
    </>
)
