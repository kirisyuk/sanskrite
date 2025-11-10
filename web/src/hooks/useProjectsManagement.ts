import { useState, useRef, useCallback } from 'react';
import { useProjectsStore } from '../stores/projectsStore';
import { useGenaiChatStore } from '../stores/genaiChatStore';

export function useProjectsManagement() {
  const [editingProjectId, setEditingProjectId] = useState<string | null>(null);
  const [editingProjectName, setEditingProjectName] = useState<string>('');
  const editingInputRef = useRef<HTMLInputElement | null>(null);
  
  const { currentProject, deleteProject, updateProject, setCurrentProject } = useProjectsStore();
  const genaiChat = useGenaiChatStore();

  const resetChatState = useCallback(() => {
    genaiChat.reset();
    setCurrentProject(null);
  }, [genaiChat, setCurrentProject]);

  const handleDeleteChat = async (projectId: string, projectName: string) => {
    if (!confirm(`Delete "${projectName}"?`)) return;
    
    try {
      // Check if the deleted chat is currently open
      const isCurrentChat =
        currentProject?.id === projectId || genaiChat.projectId === projectId;
      
      await deleteProject(projectId);
      
      // If we deleted the current chat, reset it like "New Chat"
      if (isCurrentChat) {
        resetChatState();
      }
    } catch (error) {
      console.error('Failed to delete chat:', error);
    }
  };

  const handleRenameChat = async (projectId: string, newName: string) => {
    if (!newName.trim()) {
      setEditingProjectId(null);
      return;
    }
    
    try {
      await updateProject(projectId, { displayName: newName.trim() });
      setEditingProjectId(null);
      setEditingProjectName('');
    } catch (error) {
      console.error('Failed to rename chat:', error);
    }
  };

  const startEditingProject = (projectId: string, currentName: string) => {
    setEditingProjectId(projectId);
    setEditingProjectName(currentName);
    setTimeout(() => {
      editingInputRef.current?.focus();
      editingInputRef.current?.select();
    }, 0);
  };

  return {
    editingProjectId,
    setEditingProjectId,
    editingProjectName,
    setEditingProjectName,
    editingInputRef,
    resetChatState,
    handleDeleteChat,
    handleRenameChat,
    startEditingProject,
  };
}

